package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/FutureAI/token-hub/common"
	"github.com/FutureAI/token-hub/controller"
	"github.com/FutureAI/token-hub/middleware"
	"github.com/FutureAI/token-hub/model"
	"github.com/FutureAI/token-hub/router"
	"github.com/FutureAI/token-hub/webui"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// webDist 内嵌前端构建产物，使一个二进制同时提供前端页面与 API。
//
// ⚠️ 该指令要求 web/dist 目录存在且非空，否则**编译失败**（不是运行时报错）。
// 因此在干净的 clone 上必须先构建前端：
//
//	make web    # 等价于 cd web && npm ci && npm run build
//
// 这也意味着根包无法在未构建前端的树上编译，`go test ./...` 会连带失败——
// 根包内因此不放任何测试，需要测试的逻辑一律下沉到子包，详见 Makefile。
//
// all: 前缀用于把以 . 或 _ 开头的文件也纳入，避免将来产物里出现这类文件时被静默漏掉。
//
//go:embed all:web/dist
var webDist embed.FS

// HTTP 服务器超时。
//
// 此前用的是 gin 的 server.Run()，它内部是 http.ListenAndServe——**没有任何超时**，
// 慢速发送请求体（slowloris）可以长期占用连接与 goroutine。
// WriteTimeout 必须大于最慢的处理路径：提交上游最长 30s，取 180s 留足余量。
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 180 * time.Second
	idleTimeout       = 120 * time.Second

	// shutdownTimeout 优雅关闭的等待上限。要大于最慢的在途请求，
	// 否则滚动更新时正在提交上游的请求仍会被切断。
	shutdownTimeout = 35 * time.Second
)

func main() {
	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		common.SysLog("No .env file found, using environment variables")
	}

	// 调试开关必须在加载 .env 之后、InitDB 之前应用（InitDB 会读它决定是否打印 SQL）
	common.ApplyDebugSetting()

	// 安全前置校验：密钥必须存在且足够强，否则拒绝启动（fail-closed）。
	// 绝不能回落到可预测的默认值——那会让 JWT 可被伪造、供应商密钥可被解密。
	for _, name := range []string{"JWT_SECRET", "SECRET_KEY"} {
		if _, err := common.RequireSecret(name); err != nil {
			common.FatalLog("[安全] " + err.Error())
		}
	}

	// 初始化数据库
	if err := model.InitDB(); err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
	}

	// 任务对账循环：接管所有非终态任务（含上个进程遗留的）。
	// 用一个可取消的 context 控制生命周期，关闭时能干净地停下来。
	reconcilerCtx, stopReconciler := context.WithCancel(context.Background())
	go controller.RunTaskReconciler(reconcilerCtx)

	// 设置 Gin 模式（默认 release；仅在显式 GIN_MODE=debug 时开启调试）
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建 Gin 引擎
	server := gin.New()

	// multipart 解析的内存阈值。小于请求体上限（10MB+64KB），
	// 因此上传内容始终在内存中处理，不会落临时文件。
	server.MaxMultipartMemory = common.MaxUploadFileSize

	// 信任代理配置。
	// gin 默认信任所有代理（0.0.0.0/0），导致 c.ClientIP() 无条件采信客户端自带的
	// X-Forwarded-For，登录限流可被伪造 IP 绕过。因此默认不信任任何代理头，
	// 仅在显式配置 TRUSTED_PROXIES 时才信任指定 CIDR。
	trustedProxies := common.ParseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err := server.SetTrustedProxies(trustedProxies); err != nil {
		common.FatalLog("TRUSTED_PROXIES 配置无效: " + err.Error())
	}
	if len(trustedProxies) == 0 {
		common.SysLog("[安全] TRUSTED_PROXIES 未配置，将直接使用 TCP 对端地址作为客户端 IP")
	} else {
		common.SysLogf("[安全] 信任的代理网段: %s", strings.Join(trustedProxies, ", "))
	}

	// 请求体大小限制。必须尽早挂载：JSON 绑定会把整个请求体读进内存，
	// 没有上限时一个几百 MB 的 body 就能让进程分配等量内存。
	server.Use(middleware.BodyLimit())

	// 安全响应头
	server.Use(middleware.SecurityHeaders())

	// 添加 Recovery 中间件（不向客户端泄露 panic 细节）
	server.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Printf("[PANIC] %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		c.JSON(500, gin.H{
			"error": gin.H{
				"message": "Internal server error",
				"type":    "server_error",
			},
		})
	}))

	// 添加 Logger 中间件
	// SkipQueryString 必须为 true：token / data_key 等敏感值历史上曾通过查询参数传递，
	// 若记录查询串会把凭证写进访问日志、代理日志与日志聚合平台。
	server.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipQueryString: true,
	}))

	// 设置路由
	router.SetRouter(server)

	// 挂载内嵌的前端。必须在 SetRouter 之后：webui 通过 NoRoute 接管
	// 所有未匹配的路径，也就是「API 路由优先，剩下的才可能是前端页面」。
	if err := webui.Register(server, webDist, "web/dist"); err != nil {
		common.FatalLog("挂载前端静态资源失败: " + err.Error())
	}

	// 获取端口
	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	// 验证端口
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatalf("invalid PORT value: %s", port)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           server,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// 在独立 goroutine 中监听，主 goroutine 负责等待退出信号
	go func() {
		common.SysLogf("Token Hub started on port %s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.FatalLog("failed to start server: " + err.Error())
		}
	}()

	// 优雅关闭。
	//
	// 没有这一段时，滚动更新会在收到 SIGTERM 的瞬间切断所有在途请求：
	// 客户端拿到连接重置而不是结构化错误，正在提交上游的请求则留下
	// 「已扣费但状态未落库」的痕迹。defer 也只能在正常返回时执行，
	// 被信号直接杀死时根本不会跑。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	common.SysLog("收到退出信号，开始优雅关闭（等待在途请求完成）")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		common.SysError("优雅关闭超时，仍有请求未完成: " + err.Error())
	}

	// 停对账循环并释放出站连接，避免进程退出时留下半途的查询
	stopReconciler()
	common.OutboundHTTPClient().CloseIdleConnections()

	if err := model.CloseDB(); err != nil {
		common.SysError("关闭数据库连接失败: " + err.Error())
	}

	common.SysLog("Token Hub 已退出")
}
