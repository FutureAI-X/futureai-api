# FutureAI API

下一代LLM网关

## 文档

| 我想… | 看这里 |
|---|---|
| 本地跑起来、改代码 | **[deploy/local/DEV.md](deploy/local/DEV.md)** |
| 查环境变量的含义 | [deploy/local/ENV.md](deploy/local/ENV.md) |
| 打包成镜像、用 Docker 在本地跑 | **[deploy/local/DOCKER.md](deploy/local/DOCKER.md)** |
| 在本地跑 Nginx 网关（验证部署架构） | [deploy/local/GATEWAY.md](deploy/local/GATEWAY.md) |
| 部署到服务器 | [deploy/README.md](deploy/README.md) |
| 部署出问题了 | [deploy/NOTES.md](deploy/NOTES.md) |
| 查接口 | [docs/API.md](docs/API.md) |
| Electron 客户端上传参考图 | [docs/CLIENT_IMAGE_UPLOAD.md](docs/CLIENT_IMAGE_UPLOAD.md) |

## 技术栈

| | |
|---|---|
| 后端 | Go + Gin + GORM |
| 前端 | React + TypeScript + Vite（产物内嵌进后端二进制） |
| 数据库 | PostgreSQL |
| 部署 | Docker / Docker Compose |

## 项目结构

```
futureai-api/
├── main.go              # 主入口（含 go:embed 前端产物）
├── Dockerfile           # 多阶段构建：前端 → 后端 → 运行时
├── Makefile             # 常用命令。注意 web 是 build / test 的前置
├── docker-compose.yml   # 本地开发栈：应用容器 + PostgreSQL
├── .env.example         # 环境变量模板
│
├── docs/                # 文档
│   ├── API.md           #   接口参考（认证、公开接口、管理接口、通用行为）
│   ├── client/          #   对外文档：图像/视频通用约定 + 各模型使用说明
│   └── design/          #   内部设计：图像/视频链路 + 各模型实现说明
│
├── deploy/              # 部署编排
│   ├── README.md        #   服务器部署步骤
│   ├── NOTES.md         #   为什么这么设计 / 排查 / 升级备份 / 上线自检清单
│   ├── local/           #   本地开发、Docker、环境变量与网关文档
│   ├── build-image.sh   #   构建镜像并导出，收平台参数
│   ├── backup.sh        #   数据库 + .env 定时备份（含轮转）
│   ├── restore.sh       #   从备份恢复（演练用）
│   ├── gateway/         #   Nginx 容器网关（全机唯一占用 80/443）
│   └── futureai-api/    #   生产用的应用 + PostgreSQL compose
│
├── webui/               # 托管内嵌的前端静态文件与安全头
│   ├── webui.go
│   └── webui_test.go
│
├── common/              # 公共工具
│   ├── body_limit.go    #   请求体大小上限
│   ├── crypto.go        #   密码加密
│   ├── jwt.go           #   JWT（含令牌版本号，改密即失效）
│   ├── database.go      #   数据库类型定义
│   ├── env.go           #   环境变量工具
│   ├── log.go           #   日志
│   ├── outbound.go      #   出站请求（共享连接池 + SSRF 防护 + 代理）
│   ├── paths.go         #   API 与前端路由的分界判断
│   └── trusted_proxy.go #   TRUSTED_PROXIES 解析
│
├── router/              # 路由注册
├── controller/          # 控制器（认证、模型、计费、任务、上传）
├── middleware/          # 中间件（认证、限流、安全头）
├── model/               # 数据模型与迁移
├── supplier/            # 上游供应商适配
│
└── web/                 # 前端源码
    ├── src/
    ├── index.html
    ├── package.json
    └── vite.config.ts
```
