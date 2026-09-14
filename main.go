package main

import (
	"embed"
	"log"
	"os"
	"strconv"
	"strings"

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

func main() {
	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		common.SysLog("No .env file found, using environment variables")
	}

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
	defer model.CloseDB()

	// 恢复未完成任务的轮询
	go controller.RecoverPendingTasks()

	// 设置 Gin 模式（默认 release；仅在显式 GIN_MODE=debug 时开启调试）
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建 Gin 引擎
	server := gin.New()

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

	// 启动服务器
	common.SysLogf("Token Hub started on port %s", port)
	if err := server.Run(":" + port); err != nil {
		common.FatalLog("failed to start server: " + err.Error())
	}
}
