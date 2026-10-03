# 本地开发

这份文档带你在本机把 FutureAI API 跑起来，并覆盖日常改代码、构建、跑测试的流程。
首次搭建请**从头照着做**；已经跑起来过的，按下面的表跳到需要的章节。

| 我要… | 去哪 |
|---|---|
| 打包镜像、用 Docker 在本地跑 | [DOCKER.md](DOCKER.md) |
| 在本地验证部署架构（Nginx 网关） | [GATEWAY.md](GATEWAY.md) |
| 部署到服务器 | [../deploy/README.md](../deploy/README.md) |
| 查接口 | [API.md](API.md) |

## 本文路线

| 节 | 做什么 |
|---|---|
| 一 | 装依赖：Go / Node.js / Docker Desktop |
| 二 | 配置 `.env`（密钥、数据库密码） |
| 三 | 起 PostgreSQL 容器 |
| 四 | 构建前端、启动后端、登录 |

前四节是**首次搭建**的完整流程，按顺序做。第五节往后是**增量内容** ——
测试、排查、变量速查，按需查阅。

> ⚠️ **第二节和第三节不能颠倒。** compose 解析配置时就需要 `.env` 里的
> `POSTGRES_PASSWORD`，没有它连 `docker compose up -d postgres` 都跑不起来。
> 详见第二节开头。

> **两条约定**
>
> - 所有命令都在**仓库根目录**执行（先 `cd` 到仓库根）。
> - 仓库里有 Makefile（`make web` / `make run` / `make test`），但 Windows 的
>   Git Bash 默认没有 make，所以本文一律写裸命令 —— 复制粘贴到哪儿都能跑。

---

## 一 装好要用的东西

| 依赖 | 版本 | 用途 | 检查命令 |
|---|---|---|---|
| Go | ≥ 1.27 | 编译后端 | `go version` |
| Node.js | ≥ 20 | 构建 `web/` 前端 | `node -v` |
| Docker Desktop | 能跑即可 | 跑 PostgreSQL | `docker info` |

挨个确认一遍：

```bash
go version      # 需要 go1.27.0 或更高
node -v         # 需要 v20 或更高
docker info     # 能打印出信息就是启动好了
```

- Go 低于 1.27 会拒绝编译 —— `go.mod` 里写的就是 `go 1.27.0`。
- Node 只用来构建前端。Dockerfile 的构建阶段用的是 `node:22-alpine`。
- Docker **只用来跑 PostgreSQL**。

> 不想用 Docker 也行：本机装个 PostgreSQL 15，把 `.env` 里 `SQL_DSN`
> 的主机和端口指过去即可（见 2.2）。

---

## 二 配置 .env

**这一步必须排在起数据库之前**，两个原因：

1. `docker-compose.yml` 里的 `POSTGRES_PASSWORD` 是 `${...:?}` 形式：没有 `.env`
   时 `docker compose` 连配置都解析不过去，**包括只想起数据库的
   `docker compose up -d postgres`** —— 插值发生在「选择启动哪个服务」之前，
   文件里任何一个服务缺变量都会让整条命令失败。
2. 库密码只在**数据卷首次初始化**时写进数据库。先起库、后填密码，库里留下的
   仍是旧密码，应用连不上（见 6.3 第 3 条）。

### 2.1 复制模板

```bash
cp .env.example .env
```

`.env` 已被 gitignore，干净 clone 上没有这个文件。

### 2.2 填三个密钥

| 变量 | 作用 | 生成方式 | 注意 |
|---|---|---|---|
| `JWT_SECRET` | 签发与校验登录 Token 的签名密钥 | `openssl rand -hex 32` | 换掉会让所有已签发的 Token 立即失效，用户需重新登录 |
| `SECRET_KEY` | 加密数据库里的供应商 API Key 等敏感字段 | `openssl rand -hex 32` | ⚠️ 一旦用于加密数据后**不可更改** |
| `POSTGRES_PASSWORD` | 本地开发库的密码，同时被 `SQL_DSN` 引用 | `openssl rand -hex 24` | 只能用 hex / base64url，见下 |

三条命令跑一遍：

```bash
openssl rand -hex 32   # → 粘到 JWT_SECRET=
openssl rand -hex 32   # → 粘到 SECRET_KEY=
openssl rand -hex 24   # → 粘到 POSTGRES_PASSWORD=
```

`SQL_DSN` **不用填** —— 模板里它已经引用上面三个变量，连接串会跟着一起变。
（想连别的库，比如本机装的 PostgreSQL，也只需改它的主机和端口。）

> ⚠️ **`SECRET_KEY` 一旦用于加密数据后不可更改** —— 改了，已存的供应商密钥
> 全部解不开且无法恢复。请另外抄一份存到密码管理器。

> ⚠️ **`POSTGRES_PASSWORD` 只能用 hex 或 base64url** 这类不含 `@ : / #` 的字符：
> 它会被原样拼进连接串，出现上述字符会把连接串拆坏。
> `openssl rand -hex` 生成的就是安全的。

### 2.3（可选）设一个 root 初始密码

`.env` 的 `INITIAL_ROOT_PASSWORD=` 填一个自己记得住的密码。

留空则由程序随机生成，首次启动时写进 `root_initial_password.txt`（权限 0600）。

> ⚠️ 密码里含 `$` 就必须用**单引号**包起来（`'a$b$c'`）—— 双引号和不加引号都会
> 触发变量展开，`$CX1` 这类片段被替换成空串，且**本机跑与容器跑的结果还不一样**，
> 于是数据库里存的是一个值、你登录时用的是另一个值。初始密码尽量别用 `$`。

### 2.4 确认 TRUSTED_PROXIES 留空

```bash
grep '^TRUSTED_PROXIES=' .env
```

本地直连、前面没有反向代理，输出应该是空的 `TRUSTED_PROXIES=`。
（填 `127.0.0.1/32` 也不会出错，只是没有意义：那是在告诉应用
「去相信一个并不存在的代理写的 `X-Forwarded-For`」。）

**其余变量保持默认**，含义见 `.env.example` 里的注释或本篇第七节。

---

## 三 起数据库

### 3.1 启动 PostgreSQL 容器

```bash
docker compose up -d postgres
```

⚠️ 服务名 `postgres` **不能省**。不加的话会连应用容器一起起 —— 那是 [DOCKER.md](DOCKER.md) 的用法。

### 3.2 确认它在跑

```bash
docker compose ps
```

STATUS 一列出现 `healthy` 才算好，首次启动大约要等 10 秒。

### 3.3（可选）看数据库日志

```bash
docker compose logs -f postgres
```

`Ctrl+C` 退出。数据库只绑定 `127.0.0.1:5432`，局域网里的其他机器连不上。

---

## 四 把应用跑起来

### 4.1 先构建一次前端产物

**不管后面走哪条路径，这一步都省不掉。** 根包通过 `//go:embed all:web/dist`
（见 [main.go:38](../main.go#L38)）把前端产物编进二进制，而 `go:embed` 是**编译期**
的 —— `web/dist` 不存在或为空时 `go build` 直接失败。

```bash
(cd web && npm ci && npm run build)
```

产物落在 `web/dist/`。外面那对括号是子 shell，执行完仍停在仓库根目录。

> 别被 [webui/webui.go](../webui/webui.go) 误导：那个包**不**做内嵌，它接收
> 传进来的文件系统，读不到 `index.html` 时只是页面不可用、API 照常工作。
> 编译不过的是根包，因为 `go:embed` 指令写在了 `main.go` 里。
>
> 同一个原因，`go test ./...` 也会失败（根包被一起纳入编译）→ 见第五节。

### 4.2 选一条路径启动

两种跑法，**后端是同一个进程，区别只在前端从哪儿来**：

| | 路径 A：单进程 | 路径 B：热更新 |
|---|---|---|
| 适合 | 首次搭建；验证真实产物 | 正在改前端代码 |
| 前端来自 | `web/dist`，即 4.1 的产物 | Vite 开发服务器，实时编译 |
| 怎么启动 | `go run main.go` | 终端一：`go run main.go`<br>终端二：`cd web && npm run dev` |
| 浏览器开 | http://localhost:3001 | http://localhost:5173 |
| 改完前端 | 重跑 4.1 | 即时生效 |

两条可以同时开着，端口不冲突（:3001 与 :5173）。

路径 B 下 `/api`、`/v1`、`/health` 由 Vite 代理到 :3001
（见 [web/vite.config.ts](../web/vite.config.ts)）。

> ⚠️ 走路径 B 时，**:3001 上的页面不会跟着变** —— 它提供的是上次
> `npm run build` 的产物。要让 :3001 也用上新前端，重跑 4.1。

### 4.3 确认起来了

后端启动时会打印：

```
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

首次运行会自动建表。卡在 `database migration` 或直接退出，多半是 2.2 那步的
密码没填对。

然后浏览器打开 4.2 选的那个地址，用户名 `root` 登录 —— 密码取 2.3 设的
`INITIAL_ROOT_PASSWORD`（没设就去看 `root_initial_password.txt`）。
**登录后立即改密码。**

---

## 五 跑测试和静态检查

### 5.1 两条命令

```bash
# 测试
go test $(go list -e ./... | grep -vxF "$(go list -m)")

# 静态检查
go vet $(go list -e ./... | grep -vxF "$(go list -m)")
```

后半段 `$(go list -e ./... | grep -vxF "$(go list -m)")` 的作用是**列出所有包、
再把根包剔掉**。装了 make 的话，`make test` / `make vet` 就是上面两条。

### 5.2 为什么必须剔掉根包

`go test ./...` 会把根包一起纳入编译，而根包内嵌 `web/dist` ——
没构建前端时它编译不过，于是整条命令失败。

这不损失覆盖率 —— **根包里没有任何测试文件**，所有测试都在子包中
（`common` / `controller` / `middleware` / `model` / `supplier` / `webui`）。

> **需要测试的逻辑一律下沉到子包，不要让根包重新长出 `_test.go`。**

---

## 六 出问题时的排查

前三条按文档顺序排列 —— 你走到哪一步卡住，就对号入座。

### 6.1 现象：`docker compose up -d postgres` 报 `POSTGRES_PASSWORD 必须设置`

没做第二节。先 `cp .env.example .env` 并填好 `POSTGRES_PASSWORD`。

注意补上 `.env` 后如果数据卷已经建起来了，回 6.3 第 3 条。

### 6.2 现象：编译报找不到 `web/dist`

没做第四节。回到 4.1 构建前端。

### 6.3 现象：起应用时报数据库连接失败

按顺序排查这三条，它们长得都像「密码错」：

```bash
grep -n '^POSTGRES_USER=\|^POSTGRES_DB=\|^POSTGRES_PASSWORD=\|^SQL_DSN=' .env
```

1. 某个 `POSTGRES_*` 是空的，或排在了 `SQL_DSN=` **后面**。
   godotenv 只展开位于引用行**上方**的变量，写反了不会报错 —— 静默展开成空串，
   于是连接被数据库拒绝认证。注意别为了「整理」把行序动乱了。
2. `SQL_DSN` 里变量的名字/大小写不对（只有 `[A-Z0-9_]` 会被展开，
   写成 `${postgres_password}` 同样是静默变空串）。
3. 改过 `POSTGRES_PASSWORD`，但数据卷是用旧密码初始化的 —— 密码只在数据卷
   首次初始化时写进数据库，之后改 `.env` 不会同步过去，需要进容器 `ALTER USER`
   或 `docker compose down -v` 重建（**会丢数据**）。

### 6.4 现象：Windows 上报 `listen tcp :3001: bind: ...access permissions`

**这不是端口被占用**，是 Windows 把 3001 划进了系统保留区间
（Hyper-V / WSL2 / Docker Desktop 的 `winnat` 服务干的）。
`netstat` 查不到任何进程，但就是绑不上。

```bash
netsh int ipv4 show excludedportrange protocol=tcp   # 查被保留的端口段
netsh int ipv4 show dynamicport tcp                  # 查动态端口范围的起始值
```

起始值是 **`1024`** 就是根因（Windows 默认应该是 `49152`）——
`winnat` 在 1024~15000 这段里随机保留，3001 很容易中招。

改回去（**用管理员权限的终端执行**）：

```bash
netsh int ipv4 set dynamicport tcp start=49152 num=16384
netsh int ipv6 set dynamicport tcp start=49152 num=16384
```

执行完**重启**。

或者临时绕过：改 `.env` 里的 `PORT`，同时改
[web/vite.config.ts](../web/vite.config.ts) 里三处 `target`。

---

## 七 环境变量速查（参考，不是步骤）

本地开发只需关心第二节填的那几项，其余保持默认。

| 变量名 | 描述 | 默认值 |
|--------|------|--------|
| `PORT` | 服务端口 | `3001` |
| `GIN_MODE` | Gin 模式 (debug/release) | `release` |
| `SQL_DSN` | PostgreSQL 连接字符串，引用 `${POSTGRES_USER}` / `${POSTGRES_PASSWORD}` / `${POSTGRES_DB}`（三个引用行都必须在其上方） | 必须设置 |
| `SQL_MAX_IDLE_CONNS` | 最大空闲连接数 | `10` |
| `SQL_MAX_OPEN_CONNS` | 最大打开连接数 | `80` |
| `SQL_MAX_LIFETIME` | 连接最大生存时间（秒） | `1800` |
| `SQL_MAX_IDLE_TIME` | 连接最大空闲时间（秒） | `600` |
| `JWT_SECRET` | JWT 密钥，**必填**且至少 32 字符，否则拒绝启动 | 无（缺失即退出） |
| `SECRET_KEY` | 供应商密钥加密密钥，**必填**且至少 32 字符，否则拒绝启动 | 无（缺失即退出） |
| `TRUSTED_PROXIES` | 信任的反向代理网段（逗号分隔 IP/CIDR） | 空（不信任任何代理头） |
| `API_RATE_LIMIT_PER_MINUTE` | `/v1` 每用户每分钟请求数 | `60` |
| `API_RATE_LIMIT_BURST` | `/v1` 每用户瞬时突发量 | `10` |
| `UPLOAD_CREDITS` | 图片上传的单次积分成本 | `0` |
| `OUTBOUND_PROXY` | 出站代理（可选，形如 `http://10.0.0.1:3128`） | 空（直连） |
| `INITIAL_ROOT_PASSWORD` | root 初始密码（可选，含 `$` 必须用单引号） | 空（随机生成并写入文件） |
| `DEBUG` | 调试模式，开启后打印 SQL 日志 | `false` |
| `POSTGRES_USER` | 本地开发库的用户名，供 docker compose、备份脚本与 `SQL_DSN` 使用 | `futureai_api` |
| `POSTGRES_DB` | 本地开发库的库名，同上 | `futureai_api` |
| `POSTGRES_PASSWORD` | 本地开发库的密码，供 docker compose 使用并被 `SQL_DSN` 引用 | 必须设置 |
| `FUTUREAI_API_TAG` | 本地开发栈使用的镜像标签（仅 `docker compose` 读） | `latest` |
