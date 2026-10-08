# FutureAI API 本地开发指南

## 引言

本文档说明在本机运行 FutureAI API 的流程，共四章：环境搭建、日常开发、故障排查与环境变量参考。首次搭建应按第 1 章顺序执行，各步骤存在前后依赖，不可调换。

除特别说明外，所有命令均以仓库根目录为工作目录执行。仓库虽提供 Makefile，但 Windows 平台的 Git Bash 默认不包含 make，故本文档一律给出原始命令。

其他文档：

| 需求 | 参见 |
|---|---|
| 构建镜像并以 Docker 在本地运行 | [DOCKER.md](DOCKER.md) |
| 在本地验证部署架构（Nginx 网关） | [GATEWAY.md](GATEWAY.md) |
| 部署至服务器 | [../deploy/README.md](../deploy/README.md) |
| 查询接口 | [API.md](API.md) |

---

## 1 环境搭建

### 1.1 依赖软件

开始搭建前，应确认以下软件均已正确安装且版本满足要求。

**Go** —— 用于编译后端，版本不得低于 1.27，否则编译会被拒绝。

```bash
go version
```

**Node.js** —— 用于构建前端，版本不得低于 20；服务运行时不需要。

```bash
node -v
```

**Docker Desktop** —— 用于提供 PostgreSQL，无特定版本要求。

```bash
docker info
```

若不便使用 Docker，可在本机安装 PostgreSQL 15，并将 `.env` 中 `SQL_DSN` 的主机与端口指向该实例，详见 1.2 节。

### 1.2 配置环境变量文件

环境变量集中定义于仓库根目录的 `.env` 文件中。该文件已被 gitignore 排除，刚克隆的仓库中并不存在，需由模板复制生成：

```bash
cp .env.example .env
```

> **注意**：本步骤须在 1.3 之前完成。`docker-compose.yml` 在解析配置时即需 `POSTGRES_PASSWORD`，缺失时任何 compose 命令都无法执行；且数据库密码仅在数据卷首次初始化时写入，顺序颠倒后库内保留的仍是旧密码，详见 3.3 节第 3 条。

#### 1.2.1 填写必需的密钥

`.env` 中必需填写的只有三项：`JWT_SECRET`、`SECRET_KEY`、`POSTGRES_PASSWORD`，均可由 `openssl` 生成后填入。`SQL_DSN` 无需填写，模板中已引用这三个变量，连接串会随之变化；若要连接本机安装的 PostgreSQL 等其他实例，只需修改其主机与端口。

生成 `JWT_SECRET`：

```bash
openssl rand -hex 32
```

生成 `SECRET_KEY`：

```bash
openssl rand -hex 32
```

生成 `POSTGRES_PASSWORD`：

```bash
openssl rand -hex 24
```

各变量的作用与约束如下：

| 变量 | 作用 | 约束 |
|---|---|---|
| `JWT_SECRET` | 登录 Token 的签名密钥 | 变更后所有已签发的 Token 立即失效，用户需重新登录 |
| `SECRET_KEY` | 加密数据库中供应商 API Key 等敏感字段 | 一旦用于加密数据即不可更改；变更后已存密钥无法解密且不可恢复，应另行备份至密码管理器 |
| `POSTGRES_PASSWORD` | 本地开发数据库密码，被 `SQL_DSN` 引用 | 仅可使用 hex 或 base64url 等不含 `@ : / #` 的字符，否则连接串将被破坏；`openssl rand -hex` 生成的值满足要求 |

#### 1.2.2 设置 root 初始密码（可选）

`INITIAL_ROOT_PASSWORD` 可指定为便于记忆的密码；若留空，程序将在首次启动时随机生成，并写入 `root_initial_password.txt`（权限 0600）。

> **注意**：密码中含 `$` 时必须以单引号包裹（如 `'a$b$c'`），否则变量展开会将其破坏，导致数据库中存储的密码与登录时输入的不一致。建议初始密码不使用 `$`。

#### 1.2.3 确认代理信任配置为空

本地开发为直连，前置无反向代理，故 `TRUSTED_PROXIES` 应保持为空：

```bash
grep '^TRUSTED_PROXIES=' .env
```

其余变量保持默认值，其含义参见 `.env.example` 中的注释或第 4 章。

### 1.3 启动数据库

启动 PostgreSQL 容器（服务名 `postgres` 不可省略，否则将一并启动应用容器）：

```bash
docker compose up -d postgres
```

确认其已就绪（`STATUS` 列出现 `healthy` 方为就绪，首次启动约需 10 秒）：

```bash
docker compose ps
```

查看数据库日志（`Ctrl+C` 退出）：

```bash
docker compose logs -f postgres
```

数据库仅绑定于 `127.0.0.1:5432`，局域网内其他主机无法访问。

### 1.4 构建前端并启动服务

> **注意**：本步骤不可省略，与后续采用何种运行方式无关——根包通过 `go:embed` 将前端产物编入二进制，`web/dist` 为空时编译将直接失败。

首先构建前端产物（产物位于 `web/dist/`）：

```bash
(cd web && npm ci && npm run build)
```

随后启动服务：

```bash
go run main.go
```

首次运行会自动完成建表，启动日志应包含以下内容：

```
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

若日志停留于 `database migration` 或进程直接退出，通常为 1.2 节中密码配置有误。

服务就绪后，于浏览器访问 http://localhost:3001，以用户名 `root` 及 1.2.2 节设置的 `INITIAL_ROOT_PASSWORD` 登录（未设置时可查阅 `root_initial_password.txt`）。登录后应立即修改密码。

---

## 2 日常开发

### 2.1 运行方式

后端始终为同一进程，两种运行方式的区别仅在于前端资源的来源；两者可同时运行，端口互不冲突。

**方式 A：单进程** —— 前端由 `web/dist` 提供，即 1.4 节的构建产物，适用于验证真实产物。

```bash
go run main.go
```

启动后访问 http://localhost:3001。

**方式 B：热更新** —— 前端由 Vite 开发服务器实时编译，适用于正在修改前端代码。需分别启动后端与前端，各占一个终端。

终端一，启动后端：

```bash
go run main.go
```

终端二，启动 Vite 开发服务器：

```bash
cd web && npm run dev
```

启动后访问 http://localhost:5173。该方式下 `/api`、`/v1`、`/health` 由 Vite 代理至 :3001，配置见 [web/vite.config.ts](../web/vite.config.ts)。

> **注意**：方式 B 仅作用于 :5173，:3001 上的页面不会随之更新，因该端口提供的是上一次 `npm run build` 的产物。若需 :3001 使用最新前端，应重新执行 1.4 节的构建步骤。

### 2.2 测试与静态检查

运行测试：

```bash
go test $(go list -e ./... | grep -vxF "$(go list -m)")
```

静态检查：

```bash
go vet $(go list -e ./... | grep -vxF "$(go list -m)")
```

两条命令不可简写为 `go test ./...`：根包内嵌 `web/dist`，未构建前端时会整条失败。已安装 make 时可用 `make test` 与 `make vet`。

---

## 3 故障排查

本章按现象列出常见故障，每一条均对应第 1 章中的相应步骤。

### 3.1 启动数据库时报 `POSTGRES_PASSWORD 必须设置`

原因为未执行 1.2 节。执行 `cp .env.example .env` 并填写 `POSTGRES_PASSWORD` 即可；若数据卷已建立，参见 3.3 节第 3 条。

### 3.2 编译时报找不到 `web/dist`

原因为未执行 1.4 节。重新构建前端即可。

### 3.3 启动服务时报数据库连接失败

以下三种原因的现象均表现为「密码错误」，应按顺序排查。首先检查配置：

```bash
grep -n '^POSTGRES_USER=\|^POSTGRES_DB=\|^POSTGRES_PASSWORD=\|^SQL_DSN=' .env
```

1. 某个 `POSTGRES_*` 变量为空，或其定义位于 `SQL_DSN` 之后。godotenv 仅展开位于引用行上方的变量，顺序颠倒不会报错，而会静默展开为空串，致使连接被数据库拒绝认证。因此不应为「整理」配置而调整行的顺序。
2. `SQL_DSN` 中变量名或大小写有误。仅 `[A-Z0-9_]` 形式的变量会被展开，写成 `${postgres_password}` 同样会静默展开为空串。
3. 曾修改 `POSTGRES_PASSWORD`，而数据卷仍以旧密码初始化。密码仅在数据卷首次初始化时写入数据库，此后修改 `.env` 不会同步，需进入容器执行 `ALTER USER`，或执行 `docker compose down -v` 重建（将丢失数据）。

### 3.4 Windows 上绑定 3001 端口失败

现象为 `listen tcp :3001: bind: ...access permissions`。该现象并非端口被占用，而是 Windows 将 3001 划入了系统保留区间：`netstat` 查不到任何进程，但端口无法绑定。可先确认动态端口范围的起始值（起始值为 `1024` 即为根因，Windows 默认应为 `49152`）：

```bash
netsh int ipv4 show dynamicport tcp
```

若确为上述原因，以管理员权限执行以下命令将其改回，随后重启系统。IPv4：

```bash
netsh int ipv4 set dynamicport tcp start=49152 num=16384
```

IPv6：

```bash
netsh int ipv6 set dynamicport tcp start=49152 num=16384
```

亦可临时绕过：修改 `.env` 中的 `PORT`，同时修改 [web/vite.config.ts](../web/vite.config.ts) 中的三处 `target`。

---

## 4 环境变量参考

本章为查阅性质，非操作步骤。本地开发通常只需关注 1.2 节填写的几项，其余保持默认值。

### 4.1 密钥

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `JWT_SECRET` | JWT 密钥，至少 32 字符，缺失即拒绝启动 | 无 |
| `SECRET_KEY` | 供应商密钥加密密钥，至少 32 字符，缺失即拒绝启动 | 无 |
| `POSTGRES_PASSWORD` | 本地开发数据库密码，供 docker compose 使用并被 `SQL_DSN` 引用 | 必须设置 |

### 4.2 数据库

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `SQL_DSN` | PostgreSQL 连接字符串，引用 `${POSTGRES_USER}`、`${POSTGRES_PASSWORD}`、`${POSTGRES_DB}`（三个引用行均须位于其上方） | 必须设置 |
| `POSTGRES_USER` | 本地开发数据库用户名，供 docker compose、备份脚本与 `SQL_DSN` 使用 | `futureai_api` |
| `POSTGRES_DB` | 本地开发数据库名，同上 | `futureai_api` |
| `SQL_MAX_IDLE_CONNS` | 最大空闲连接数 | `10` |
| `SQL_MAX_OPEN_CONNS` | 最大打开连接数 | `80` |
| `SQL_MAX_LIFETIME` | 连接最大生存时间（秒） | `1800` |
| `SQL_MAX_IDLE_TIME` | 连接最大空闲时间（秒） | `600` |

### 4.3 服务与部署

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `PORT` | 服务端口 | `3001` |
| `GIN_MODE` | Gin 运行模式（debug / release） | `release` |
| `DEBUG` | 调试模式，开启后打印 SQL 日志 | `false` |
| `TRUSTED_PROXIES` | 信任的反向代理网段（逗号分隔 IP / CIDR） | 空（不信任任何代理头） |
| `OUTBOUND_PROXY` | 出站代理，形如 `http://10.0.0.1:3128` | 空（直连） |
| `INITIAL_ROOT_PASSWORD` | root 初始密码，含 `$` 时须用单引号 | 空（随机生成并写入文件） |
| `FUTUREAI_API_TAG` | 本地开发栈使用的镜像标签，仅 `docker compose` 读取 | `latest` |

### 4.4 限流与计费

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `API_RATE_LIMIT_PER_MINUTE` | `/v1` 每用户每分钟请求数 | `60` |
| `API_RATE_LIMIT_BURST` | `/v1` 每用户瞬时突发量 | `10` |
| `UPLOAD_CREDITS` | 图片上传的单次积分成本 | `0` |
