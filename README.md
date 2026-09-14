# Token Hub

下一代LLM网关和AI资产管理系统

## PostgreSQL 管理

### 配置文件

`docker-compose.yml`：

```yaml
services:
  postgres:
    image: postgres:15
    container_name: token-hub-postgres
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-token_hub}
      # 密码从环境变量读取，不在仓库中硬编码
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD 必须设置}
      POSTGRES_DB: ${POSTGRES_DB:-token_hub}
    ports:
      # 仅绑定回环地址，切勿暴露到公网
      - "127.0.0.1:5432:5432"
    volumes:
      - pg_data:/var/lib/postgresql/data

volumes:
  pg_data:
```

### 常用命令

```bash
# 启动（后台运行）
docker-compose up -d

# 停止
docker-compose down

# 停止并删除数据卷（慎用，会清除所有数据）
docker-compose down -v

# 查看运行状态
docker-compose ps

# 查看日志
docker-compose logs postgres

# 实时跟踪日志
docker-compose logs -f postgres

# 重启
docker-compose restart postgres
```

### 连接信息

| 项目 | 值 |
|------|-----|
| 主机 | `localhost` |
| 端口 | `5432` |
| 用户名 | `token_hub` |
| 密码 | `token_hub_123` |
| 数据库 | `token_hub` |
| 连接字符串 | `postgres://token_hub:token_hub_123@localhost:5432/token_hub?sslmode=disable` |

### 数据持久化

- 数据存储在 Docker 卷 `pg_data` 中
- 执行 `docker-compose down` 不会丢失数据
- 只有执行 `docker-compose down -v` 才会删除数据

### 使用 pgAdmin 管理（可选）

如需图形化管理工具，可在 `docker-compose.yml` 中添加：

```yaml
services:
  # ... postgres 配置 ...

  pgadmin:
    image: dpage/pgadmin4
    container_name: token-hub-pgadmin
    environment:
      PGADMIN_DEFAULT_EMAIL: admin@admin.com
      PGADMIN_DEFAULT_PASSWORD: admin
    ports:
      - "5050:80"
```

访问 http://localhost:5050 即可使用 pgAdmin。

## 快速开始

### 1. 启动 PostgreSQL

```bash
docker-compose up -d
```

### 2. 配置环境变量

创建 `.env` 文件：

```bash
SQL_DSN=postgres://token_hub:token_hub_123@localhost:5432/token_hub?sslmode=disable
PORT=3001
GIN_MODE=release

# 必填！至少 32 字符，否则服务拒绝启动。生成: openssl rand -hex 32
JWT_SECRET=
SECRET_KEY=
```

### 3. 安装依赖

```bash
go mod tidy
```

### 4. 构建前端

```bash
cd web && npm install && npm run build && cd ..
```

> ⚠️ **这一步不能跳过。** 后端通过 `//go:embed all:web/dist` 把前端产物编进二进制，
> 该目录不存在或为空时 `go build` 会**直接编译失败**（是编译期错误，不是运行时提示）。
> 也就是说，在干净的 clone 上，任何编译根包的命令都必须先构建前端。

### 5. 运行后端

```bash
go run main.go
```

访问 http://localhost:3001 即可 —— 前端页面与 API 现在由同一个进程提供，
不需要额外的 Nginx。

### 6. 运行前端开发服务器（可选）

改前端时用这个，有热更新：

```bash
cd web
npm run dev
```

开发服务器在 http://localhost:5173 启动，并自动代理 API 请求到后端。
此时后端提供的页面是上次 `npm run build` 的产物，改前端不必重新编译后端。

### 环境变量

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `PORT` | 服务端口 | `3001` |
| `GIN_MODE` | Gin模式 (debug/release) | `release` |
| `SQL_DSN` | PostgreSQL 连接字符串 | 必须设置 |
| `SQL_MAX_IDLE_CONNS` | 最大空闲连接数 | `10` |
| `SQL_MAX_OPEN_CONNS` | 最大打开连接数 | `100` |
| `SQL_MAX_LIFETIME` | 连接最大生存时间(秒) | `60` |
| `JWT_SECRET` | JWT 密钥，**必填**且至少 32 字符，否则拒绝启动 | 无（缺失即退出） |
| `SECRET_KEY` | 供应商密钥加密密钥，**必填**且至少 32 字符 | 无（缺失即退出） |
| `TRUSTED_PROXIES` | 信任的反向代理网段（逗号分隔 IP/CIDR） | 空（不信任任何代理头） |
| `API_RATE_LIMIT_PER_MINUTE` | `/v1` 每用户每分钟请求数 | `60` |
| `API_RATE_LIMIT_BURST` | `/v1` 每用户瞬时突发量 | `10` |
| `INITIAL_ROOT_PASSWORD` | root 初始密码（可选） | 空（随机生成并写入文件） |
| `DEBUG` | 调试模式 | `false` |

## API 接口

### 用户登录

```bash
POST /api/auth/login
```

请求体：

```json
{
  "username": "root",
  "password": "<首次启动时生成，见下方说明>"
}
```

首次启动且 `users` 表为空时会自动创建 `root` 用户，其初始密码：

- 若设置了 `INITIAL_ROOT_PASSWORD` 环境变量，则使用该值；
- 否则随机生成并写入 `./root_initial_password.txt`（权限 `0600`）。

该密码**不会**写入日志。请登录后立即通过 `PUT /api/user/password` 修改，并删除密码文件。

响应示例：

```json
{
  "success": true,
  "message": "登录成功",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user": {
      "id": 1,
      "username": "root",
      "display_name": "Root User",
      "role": 100,
      "status": 1,
      "email": "",
      "credits": 100000000,
      "used_credits": 0
    }
  }
}
```

### 获取用户信息（需要登录）

```bash
GET /api/user/info
Authorization: Bearer <token>
```

### 获取模型列表

```bash
GET /v1/models
```

响应示例：

```json
{
  "object": "list",
  "data": [
    {
      "id": "deepseek-v4-flash",
      "object": "model",
      "owned_by": "token-hub"
    }
  ]
}
```

### 健康检查

```bash
GET /health
```

响应示例：

```json
{
  "status": "ok"
}
```

## 技术栈

- **后端**: Go + Gin
- **前端**: React + TypeScript + Vite（产物经 `go:embed` 编入后端二进制）
- **数据库**: PostgreSQL
- **缓存**: 待定
- **部署**: Docker / Docker Compose

## 项目结构

```
token-hub/
├── main.go              # 主入口文件（含 go:embed 前端产物）
├── Dockerfile           # 多阶段构建：前端 → 后端 → 运行时
├── Makefile             # 常用命令（注意 web 是 build/test 的前置）
├── .dockerignore
├── deploy/              # 部署编排，见 deploy/README.md
│   ├── gateway/         # Nginx 容器网关（全机唯一占用 80/443）
│   ├── token-hub/       # 应用 + PostgreSQL 的 compose
│   ├── new-api/         # 接入网关的说明
│   └── sub2api/         # 接入网关的说明
├── webui/               # 内嵌前端静态文件的托管与安全头
│   ├── webui.go
│   └── webui_test.go
├── common/              # 公共工具
│   ├── database.go      # 数据库类型定义
│   ├── env.go           # 环境变量工具
│   ├── log.go           # 日志工具
│   ├── crypto.go        # 密码加密工具
│   ├── jwt.go           # JWT Token 工具
│   ├── paths.go         # API 与前端路由的分界判断
│   └── trusted_proxy.go # TRUSTED_PROXIES 解析
├── router/              # 路由配置
│   └── router.go
├── controller/          # 控制器
│   ├── model.go         # 模型控制器
│   └── auth.go          # 认证控制器
├── middleware/           # 中间件
│   └── auth.go          # 认证中间件
├── model/               # 数据模型
│   ├── main.go          # 数据库初始化
│   ├── user.go          # 用户模型
│   └── model.go         # 模型管理
├── web/                 # 前端项目
│   ├── src/
│   │   ├── App.tsx      # 主页组件
│   │   ├── App.css      # 主页样式
│   │   ├── index.css    # Tailwind CSS
│   │   ├── lib/utils.ts # 工具函数
│   │   └── main.tsx     # 入口文件
│   ├── index.html       # HTML模板
│   ├── package.json     # 前端依赖
│   └── vite.config.ts   # Vite配置
├── docker-compose.yml   # Docker Compose 配置（PostgreSQL）
├── .env.example         # 环境变量示例
├── go.mod               # Go模块文件
└── go.sum               # 依赖校验文件
```

## 主页功能

现代化的主页 UI，参考 New API 项目设计：

- ✅ 顶部导航栏（毛玻璃效果、响应式）
- ✅ Hero 区域（渐变背景、网格图案、动画效果）
- ✅ 终端演示（实时显示 API 响应）
- ✅ 功能特性展示（Bento 网格布局）
- ✅ 模型列表（卡片式设计、悬停效果）
- ✅ 快速开始代码示例
- ✅ 暗色模式支持（跟随系统）
- ✅ 响应式设计（支持移动端）
- ✅ 流畅动画（淡入效果）

## 上线安全清单

部署到公网前请逐项确认：

- [ ] `JWT_SECRET` 与 `SECRET_KEY` 均设置为至少 32 字符的随机值（`openssl rand -hex 32`）。
      服务在两者缺失或过短时会**拒绝启动**，不会回落到默认值。
      ⚠️ `SECRET_KEY` 一旦用于加密数据后不可更改，否则已存的供应商密钥将无法解密。
- [ ] `GIN_MODE=release`、`DEBUG=false`。
- [ ] PostgreSQL 端口**不要**暴露到公网（`docker-compose.yml` 已默认绑定 `127.0.0.1`），
      并使用强密码。
- [ ] 服务置于 HTTPS 反向代理之后，由代理下发 `Strict-Transport-Security`。
- [ ] 若部署在反向代理/CDN 之后，设置 `TRUSTED_PROXIES` 为代理网段，
      否则所有请求的客户端 IP 都会是代理地址，登录限流会把全部用户视为同一来源。
      反之，若服务直接对外，**不要**设置该变量（默认不信任任何代理头，可防 `X-Forwarded-For` 伪造）。
- [ ] 按业务规模调整 `API_RATE_LIMIT_PER_MINUTE` / `API_RATE_LIMIT_BURST`。
- [ ] 确认每个启用中的模型都配置了计费规则：未配置规则的模型会返回
      「该模型未配置计费规则，暂不可用」，而不是静默免费。
- [ ] 登录后立即修改 root 密码，并删除 `root_initial_password.txt`。
- [ ] 配置日志轮转（访问日志已开启 `SkipQueryString`，不会记录查询串）。

### 反向代理

**前端静态文件由 Go 服务自己提供**（`web/dist` 通过 `go:embed` 编进二进制，
见 [webui/](webui/)），Nginx 不再需要托管静态文件。它只负责 TLS 终止、
按域名分流、gzip、以及登录接口的限流。

完整的部署编排见 [deploy/README.md](deploy/README.md)：同一台服务器上
token-hub、new-api、sub2api 三个服务共用一个 Nginx 容器网关，各自独立 compose。

网关配置的唯一来源是 [deploy/gateway/](deploy/gateway/)。

### 凭证传递方式

所有凭证**仅**通过 `Authorization: Bearer <token>` 请求头传递。

查询参数形式（`?token=` / `?data_key=`）已不再支持：查询串会进入访问日志、
反向代理日志、浏览器历史与 `Referer` 头。获取 API Key 列表时请使用
`X-Data-Key` 请求头。

## 测试

```bash
make test              # 运行全部单元测试
```

覆盖范围包括密钥强度校验、加解密往返、SSRF 目标拦截、
文件名清洗与 multipart 头注入防护、认证凭证提取、限流令牌桶、
客户端 IP 还原、以及前端静态服务的 SPA 回退与缓存策略。

> **为什么不是 `go test ./...`**：根包 `main` 内嵌 `web/dist`，
> 没构建前端时它编译不过，而 `go test ./...` 会把根包一起纳入编译，
> 于是整条命令失败。`make test` 显式排除了根包。
>
> 这不损失覆盖率 —— 根包里**没有**任何测试文件，所有测试都在子包中
> （`common` / `webui` / `middleware` / `controller` / `model`）。
> 需要测试的逻辑一律下沉到子包，不要让根包重新长出 `_test.go`。
>
> 同样的道理，`go build ./...` 和 `go vet ./...` 也需要先构建前端。

## 开发计划

- [x] PostgreSQL 数据库集成
- [x] 模型管理（从数据库读取）
- [x] 用户认证系统（JWT + bcrypt）
- [x] API Key 管理
- [x] 使用量统计
- [x] 计费系统（预扣费 + 失败退款）