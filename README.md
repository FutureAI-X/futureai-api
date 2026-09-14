# Token Hub

下一代LLM网关和AI资产管理系统

## 本地开发数据库

`docker-compose.yml` 提供本地开发用的 PostgreSQL，只绑定 `127.0.0.1:5432`，不对外：

```bash
docker compose up -d              # 启动
docker compose down               # 停止，数据保留在卷里
docker compose down -v            # 连数据一起删，慎用
docker compose logs -f postgres   # 看日志
```

密码在 `.env` 的 `POSTGRES_PASSWORD`，连接串在 `SQL_DSN`。

> 这是**开发用**的库。生产环境用的那一份在 [deploy/token-hub/](deploy/token-hub/)，
> 它刻意不发布任何端口 —— 两者目录名相同但项目名不同，不会互相干扰。

## 快速开始

```bash
# 1. 本地数据库
docker compose up -d

# 2. 环境变量：复制模板后填三个值
cp .env.example .env
openssl rand -hex 32      # -> JWT_SECRET
openssl rand -hex 32      # -> SECRET_KEY
openssl rand -hex 24      # -> POSTGRES_PASSWORD（同时替换 SQL_DSN 里的 CHANGE_ME）

# 3. 构建前端
make web                  # 等价于 cd web && npm ci && npm run build

# 4. 启动
make run                  # 等价于 go run main.go
```

访问 http://localhost:3001 —— 前端页面与 API 由同一个进程提供，不需要额外的 Nginx。

> ⚠️ **第 3 步不能跳过。** 后端通过 `//go:embed all:web/dist` 把前端产物编进二进制，
> 该目录不存在或为空时 `go build` 会**直接编译失败**（是编译期错误，不是运行时提示）。
> 干净的 clone 上，任何编译根包的命令都必须先构建前端。

改前端时用热更新：`cd web && npm run dev`（:5173，自动代理 API 到后端）。
此时后端提供的仍是上次构建的产物，改前端不必重新编译后端。

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

完整参考见 [docs/API.md](docs/API.md)。速览：

| 前缀 | 凭证 | 内容 |
|---|---|---|
| `/health` | 无 | 健康检查 |
| `/api/auth`、`/api/pricing` | 无 | 登录、计费规则 |
| `/api/user` | JWT | 用户信息、API Key、积分流水、任务记录 |
| `/api/admin` | JWT + role 100 | 用户/供应商/模型/端点/计费规则管理 |
| `/v1` | `sk-` API Key | OpenAI 兼容：模型列表、图像生成、图片上传、任务查询 |

所有凭证**仅**通过 `Authorization: Bearer <...>` 请求头传递，不支持查询参数。

首次启动且 `users` 表为空时会自动创建 `root` 用户：密码取 `INITIAL_ROOT_PASSWORD`，
未设置则随机生成并写入 `./root_initial_password.txt`（权限 `0600`，不写日志）。
**登录后立即改密。**

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
├── deploy/              # 部署编排
│   ├── README.md        # 照这个顺序敲
│   ├── NOTES.md         # 为什么这么设计 / 排查 / 升级备份
│   ├── build-image.sh   # 本机构建镜像并导出，收平台参数
│   ├── gateway/         # Nginx 容器网关（全机唯一占用 80/443）
│   └── token-hub/       # 应用 + PostgreSQL 的 compose
├── docs/
│   └── API.md           # 接口参考
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

## 部署

完整编排见 [deploy/README.md](deploy/README.md)：同一台服务器上 token-hub、
new-api、sub2api 三个服务共用一个 Nginx 容器网关，各自独立 compose。
网关配置的唯一来源是 [deploy/gateway/](deploy/gateway/)。

**前端静态文件由 Go 服务自己提供**（`web/dist` 通过 `go:embed` 编进二进制，
见 [webui/](webui/)），Nginx 不再托管静态文件 —— 只负责 TLS 终止、
按域名分流、gzip 和登录接口限流。

上线前的安全自检清单、升级/回滚/备份流程、排查表都在 [deploy/NOTES.md](deploy/NOTES.md)。

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