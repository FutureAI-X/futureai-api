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
| 四 | 构建前端产物 |
| 五 | 启动后端并登录 |

前五节是**首次搭建**的完整流程，按顺序做。第六节往后是**增量内容** ——
热更新、测试、排查、变量速查，按需查阅。

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

> 不想用 Docker 也行：本机装个 PostgreSQL 15，把 `.env` 里的 `SQL_DSN`
> 指过去即可（见 2.3）。

---

## 二 配置 .env

**这一步必须排在起数据库之前**，两个原因：

1. `docker-compose.yml` 里的 `POSTGRES_PASSWORD` 是 `${...:?}` 形式：没有 `.env`
   时 `docker compose` 连配置都解析不过去，**包括只想起数据库的
   `docker compose up -d postgres`** —— 插值发生在「选择启动哪个服务」之前，
   文件里任何一个服务缺变量都会让整条命令失败。
2. 库密码只在**数据卷首次初始化**时写进数据库。先起库、后填密码，库里留下的
   仍是旧密码，应用连不上（见 8.3 第 3 条）。

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

> ⚠️ **`SECRET_KEY` 一旦用于加密数据后不可更改** —— 改了，已存的供应商密钥
> 全部解不开且无法恢复。请另外抄一份存到密码管理器。

> ⚠️ **`POSTGRES_PASSWORD` 只能用 hex 或 base64url** 这类不含 `@ : / #` 的字符：
> 它会被原样拼进连接串，出现上述字符会把连接串拆坏。
> `openssl rand -hex` 生成的就是安全的。

### 2.3 确认 SQL_DSN（通常不用改）

`.env.example` 里的 `SQL_DSN` 已经写成引用三个 `POSTGRES_*` 的形式：

```
SQL_DSN=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/${POSTGRES_DB}?sslmode=disable
```

所以 2.2 填完密码，连接串就跟着变了，**不用改两处**。用户名与库名的默认值都是
`futureai_api`，想换名字也只改 `POSTGRES_USER` / `POSTGRES_DB` 这两行即可。

这一步只需确认一件事：

```bash
grep -n '^POSTGRES_USER=\|^POSTGRES_DB=\|^POSTGRES_PASSWORD=\|^SQL_DSN=' .env
```

三个 `POSTGRES_*` 都必须排在 `SQL_DSN=` **前面**。解析 `.env` 的 godotenv 只展开
位于引用行**上方**的变量，也仅限 `[A-Z0-9_]` 的变量名。写反了、或者写成小写的
`${postgres_password}`，都不会报错 —— 被静默展开成空串，现象是连接被数据库
拒绝认证，看起来像密码填错。

> 想连别的库（本机装的 PostgreSQL、或别的容器）时，改 `SQL_DSN` 的
> 主机 / 端口即可；用户名密码库名仍跟着上面三行走，要么把那个库的账号设成
> 一样，要么把这条改回明文写死。

### 2.4（可选）设一个 root 初始密码

`.env` 的 `INITIAL_ROOT_PASSWORD=` 填一个自己记得住的密码。

留空则由程序随机生成，首次启动时写进 `root_initial_password.txt`（权限 0600）。

> ⚠️ 密码里含 `$` 就必须用**单引号**包起来（`'a$b$c'`）—— 双引号和不加引号都会
> 触发变量展开，`$CX1` 这类片段被替换成空串，且**本机跑与容器跑的结果还不一样**，
> 于是数据库里存的是一个值、你登录时用的是另一个值。初始密码尽量别用 `$`。

### 2.5 确认 TRUSTED_PROXIES 留空

```bash
grep '^TRUSTED_PROXIES=' .env
```

本地直连、前面没有反向代理，输出应该是空的 `TRUSTED_PROXIES=`。
（填 `127.0.0.1/32` 也不会出错，只是没有意义：那是在告诉应用
「去相信一个并不存在的代理写的 `X-Forwarded-For`」。）

**其余变量保持默认**，含义见 `.env.example` 里的注释或本篇第九节。

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

## 四 构建前端

### 4.1 装依赖并构建

```bash
cd web && npm ci && npm run build
```

产物落在 `web/dist/`。

### 4.2 回到仓库根目录

```bash
cd ..
```

### 4.3 ⚠️ 这一步不能跳过

根包通过 `//go:embed all:web/dist`（见 [main.go:38](../main.go#L38)）把前端产物编进
二进制。**`web/dist` 不存在或为空时 `go build` 直接编译失败** ——
是编译期错误，不是运行时提示。

由此带来两个后果：

- 干净的 clone 上，任何编译根包的命令都必须先做本节
- `go test ./...` 同样会失败（它会把根包一起纳入编译）→ 见第七节

> 别被 [webui/webui.go](../webui/webui.go) 误导：那个包**不**做内嵌，它接收
> 传进来的文件系统，读不到 `index.html` 时只是页面不可用、API 照常工作。
> 编译不过的是根包，因为 `go:embed` 指令写在 `main.go` 里。

---

## 五 启动后端

### 5.1 启动

```bash
go run main.go
```

首次运行会自动建表。

### 5.2 对着日志确认起来了

```
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

卡在 `database migration` 或直接退出，多半是 2.2 / 2.3 两步的密码不一致。

### 5.3 打开页面

浏览器访问 **http://localhost:3001**。

前端页面与 API 由同一个进程提供，不需要额外的 Nginx。

### 5.4 登录并立即改密

用户名 `root`，密码取 2.4 设的 `INITIAL_ROOT_PASSWORD`
（没设就去看 `root_initial_password.txt`）。**登录后立即改密码。**

---

## 六 改前端时开热更新

第四节那种「改一次构建一次」太慢，改前端时开热更新。开两个终端：

| 终端 | 命令 | 作用 |
|---|---|---|
| 一 | `go run main.go` | 后端，监听 :3001 |
| 二 | `cd web && npm run dev` | Vite 开发服务器，监听 :5173 |

然后浏览器改开 **http://localhost:5173** —— 改前端代码即时生效。

`/api`、`/v1`、`/health` 由 Vite 代理到 :3001（见 [web/vite.config.ts](../web/vite.config.ts)）。

> ⚠️ **:3001 上的页面不会跟着变。** 它提供的是上次 `npm run build` 的产物，
> 要让 :3001 也用上新前端，回第四节重新构建。

---

## 七 跑测试和静态检查

### 7.1 两条命令

```bash
# 测试
go test $(go list -e ./... | grep -vxF "$(go list -m)")

# 静态检查
go vet $(go list -e ./... | grep -vxF "$(go list -m)")
```

后半段 `$(go list -e ./... | grep -vxF "$(go list -m)")` 的作用是**列出所有包、
再把根包剔掉**。装了 make 的话，`make test` / `make vet` 就是上面两条。

### 7.2 为什么必须剔掉根包

`go test ./...` 会把根包一起纳入编译，而根包内嵌 `web/dist` ——
没构建前端时它编译不过，于是整条命令失败。

这不损失覆盖率 —— **根包里没有任何测试文件**，所有测试都在子包中
（`common` / `controller` / `middleware` / `model` / `supplier` / `webui`）。

> **需要测试的逻辑一律下沉到子包，不要让根包重新长出 `_test.go`。**

---

## 八 出问题时的排查

前三条按文档顺序排列 —— 你走到哪一步卡住，就对号入座。

### 8.1 现象：`docker compose up -d postgres` 报 `POSTGRES_PASSWORD 必须设置`

没做第二节。先 `cp .env.example .env` 并填好 `POSTGRES_PASSWORD`。

注意补上 `.env` 后如果数据卷已经建起来了，回 8.3 第 3 条。

### 8.2 现象：编译报找不到 `web/dist`

没做第四节。回到 4.1 构建前端。

### 8.3 现象：起应用时报数据库连接失败

按顺序排查这三条，它们长得都像「密码错」：

```bash
grep -n '^POSTGRES_USER=\|^POSTGRES_DB=\|^POSTGRES_PASSWORD=\|^SQL_DSN=' .env
```

1. 某个 `POSTGRES_*` 是空的，或排在了 `SQL_DSN=` 后面 —— 先回 2.3。
2. `SQL_DSN` 里变量的名字/大小写不对（只有 `[A-Z0-9_]` 会被展开）。
3. 改过 `POSTGRES_PASSWORD`，但数据卷是用旧密码初始化的 —— 密码只在数据卷
   首次初始化时写进数据库，之后改 `.env` 不会同步过去，需要进容器 `ALTER USER`
   或 `docker compose down -v` 重建（**会丢数据**）。

### 8.4 现象：Windows 上报 `listen tcp :3001: bind: ...access permissions`

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

## 九 环境变量速查（参考，不是步骤）

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
