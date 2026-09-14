# Token Hub

下一代LLM网关和AI资产管理系统

前端（React + Vite）的构建产物通过 `go:embed` 编进 Go 二进制，
因此**一个容器里同时装着前端和后端**，对外只需要一个端口，不需要额外的 Nginx
托管静态文件 —— 与 new-api、sub2api 的形态一致。

## 文档

| 我想… | 看这里 |
|---|---|
| 本地跑起来、改代码 | **[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)** |
| 打包成镜像、用 Docker 在本地跑 | **[docs/DOCKER.md](docs/DOCKER.md)** |
| 部署到服务器 | [deploy/README.md](deploy/README.md) |
| 部署出问题了 | [deploy/NOTES.md](deploy/NOTES.md) |
| 查接口 | [docs/API.md](docs/API.md) |

## 技术栈

| | |
|---|---|
| 后端 | Go + Gin + GORM |
| 前端 | React + TypeScript + Vite（产物内嵌进后端二进制） |
| 数据库 | PostgreSQL |
| 部署 | Docker / Docker Compose |

## 项目结构

```
token-hub/
├── main.go              # 主入口（含 go:embed 前端产物）
├── Dockerfile           # 多阶段构建：前端 → 后端 → 运行时
├── Makefile             # 常用命令。注意 web 是 build / test 的前置
├── docker-compose.yml   # 本地开发栈：应用容器 + PostgreSQL
├── .env.example         # 环境变量模板
│
├── docs/                # 文档
│   ├── DEVELOPMENT.md   #   本地开发
│   ├── DOCKER.md        #   打包镜像 & 本地 Docker 运行
│   └── API.md           #   接口参考
│
├── deploy/              # 服务器部署编排
│   ├── README.md        #   照着敲的步骤
│   ├── NOTES.md         #   为什么这么设计 / 排查 / 升级备份
│   ├── build-image.sh   #   构建镜像并导出，收平台参数
│   ├── gateway/         #   Nginx 容器网关（全机唯一占用 80/443）
│   └── token-hub/       #   生产用的应用 + PostgreSQL compose
│
├── webui/               # 托管内嵌的前端静态文件与安全头
│   ├── webui.go
│   └── webui_test.go
│
├── common/              # 公共工具
│   ├── crypto.go        #   密码加密
│   ├── jwt.go           #   JWT
│   ├── database.go      #   数据库类型定义
│   ├── env.go           #   环境变量工具
│   ├── log.go           #   日志
│   ├── outbound.go      #   出站请求（SSRF 防护）
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

## 开发计划

- [x] PostgreSQL 数据库集成
- [x] 模型管理（从数据库读取）
- [x] 用户认证系统（JWT + bcrypt）
- [x] API Key 管理
- [x] 使用量统计
- [x] 计费系统（预扣费 + 失败退款）
- [x] 图像生成与图片上传
- [ ] 缓存
