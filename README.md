# FutureAI API

下一代LLM网关

## 文档

| 我想… | 看这里 |
|---|---|
| 本地跑起来、改代码 | **[docs/dev/DEV.md](docs/dev/DEV.md)** |
| 查环境变量的含义 | [docs/dev/ENV.md](docs/dev/ENV.md) |
| 打包成镜像、用 Docker 在本地跑 | **[docs/dev/DOCKER.md](docs/dev/DOCKER.md)** |
| 在本地跑 Nginx 网关（验证部署架构） | [docs/dev/GATEWAY.md](docs/dev/GATEWAY.md) |
| 部署到服务器 | [deploy/README.md](deploy/README.md) |
| 部署出问题了 | [deploy/NOTES.md](deploy/NOTES.md) |
| 客户端 API：图像、视频、任务、文件、版本、模型 | [docs/api/README.md](docs/api/README.md) |
| Web 前端 API：登录、用户、管理端、计费 | [docs/web/README.md](docs/web/README.md) |
| 图像/视频生成的内部设计 | [docs/design/image/README.md](docs/design/image/README.md)、[docs/design/video/README.md](docs/design/video/README.md) |
| Electron 客户端上传参考图 | [docs/api/file-upload/README.md](docs/api/file-upload/README.md) |

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
│   ├── dev/             #   开发环境：本地开发、Docker、环境变量、网关、写作规范
│   ├── api/             #   客户端 API：图像/视频/任务/文件/版本/模型
│   ├── web/             #   Web 前端 API：登录、用户、管理端、计费
│   └── design/          #   内部设计：图像/视频链路与各模型实现
│
├── deploy/              # 服务器部署编排
│   ├── README.md        #   服务器部署步骤
│   ├── NOTES.md         #   为什么这么设计 / 排查 / 升级备份 / 上线自检清单
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
