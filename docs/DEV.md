# 本地开发

日常改代码看这份。打包镜像、用 Docker 跑 → [DOCKER.md](DOCKER.md)；
部署到服务器 → [../deploy/README.md](../deploy/README.md)。

**所有命令都在仓库根目录执行**（先 `cd` 到仓库根再开始）。

> 仓库里有 Makefile，`make web` / `make run` / `make test` 是下面这些命令的快捷方式。
> 但 Windows 的 Git Bash 默认没有 make，所以本文一律用裸命令 —— 复制粘贴到哪儿都能跑。

---

## 一 装好要用的东西

### 1.1 确认 Go 版本 ≥ 1.27

```bash
go version
```

`go.mod` 里写的是 `go 1.27.0`，低于它会拒绝编译。

### 1.2 确认 Node.js 版本 ≥ 20

```bash
node -v
```

只用来构建 `web/` 前端。

### 1.3 装 Docker Desktop 并启动

```bash
docker info
```

**只用来跑 PostgreSQL**。能打印出信息就是启动好了。

> 不想用 Docker 也行 —— 本机装个 PostgreSQL 15，把 `.env` 里的 `SQL_DSN`
> 指过去即可（见 3.5）。

---

## 二 起数据库

### 2.1 启动 PostgreSQL 容器

```bash
docker compose up -d postgres
```

⚠️ 服务名 `postgres` **不能省**。不加的话会连应用容器一起起 —— 那是 [DOCKER.md](DOCKER.md) 的用法。

### 2.2 确认它在跑

```bash
docker compose ps
```

STATUS 一列出现 `healthy` 才算好，首次启动大约要等 10 秒。

### 2.3（可选）看数据库日志

```bash
docker compose logs -f postgres
```

`Ctrl+C` 退出。数据库只绑定 `127.0.0.1:5432`，局域网里的其他机器连不上。

---

## 三 配置 .env

### 3.1 从模板复制一份

```bash
cp .env.example .env
```

### 3.2 生成 JWT_SECRET

```bash
openssl rand -hex 32
```

复制输出，填到 `.env` 的 `JWT_SECRET=` 后面。

### 3.3 生成 SECRET_KEY

```bash
openssl rand -hex 32
```

复制输出，填到 `.env` 的 `SECRET_KEY=` 后面。

> ⚠️ 这个密钥用来加密数据库里的供应商 API Key。
> **一旦用于加密数据后不可更改** —— 改了已存的密钥全部解不开且无法恢复。
> 请另外抄一份存到密码管理器。

### 3.4 生成数据库密码

```bash
openssl rand -hex 24
```

复制输出，填到 `.env` 的 `POSTGRES_PASSWORD=` 后面。

### 3.5 不需要再填 SQL_DSN

`.env.example` 里的 `SQL_DSN` 已经写成引用三个 `POSTGRES_*` 的形式：

```
SQL_DSN=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/${POSTGRES_DB}?sslmode=disable
```

所以 3.4 填完密码，连接串就跟着变了，**不用改两处**。用户名与库名的默认值都是
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

### 3.6（可选）设一个 root 初始密码

`.env` 的 `INITIAL_ROOT_PASSWORD=` 填一个自己记得住的密码。

留空则由程序随机生成，首次启动时写进 `root_initial_password.txt`。

> ⚠️ 密码里含 `$` 就必须用**单引号**包起来（`'a$b$c'`）—— 双引号和不加引号都会
> 触发变量展开，`$CX1` 这类片段被替换成空串，且**本机跑与容器跑的结果还不一样**，
> 于是数据库里存的是一个值、你登录时用的是另一个值。初始密码尽量别用 `$`。

### 3.7 确认 TRUSTED_PROXIES 留空

```bash
grep '^TRUSTED_PROXIES=' .env
```

本地直连、前面没有反向代理，输出应该是空的 `TRUSTED_PROXIES=`。
（填 `127.0.0.1/32` 也不会出错，只是没有意义：那是在告诉应用
「去相信一个并不存在的代理写的 `X-Forwarded-For`」。）

**其余变量保持默认**，含义见 `.env.example` 里的注释或本篇第九节。

---

## 四 构建前端

### 4.1 进前端目录并装依赖

```bash
cd web && npm ci
```

### 4.2 构建前端产物

```bash
npm run build
```

产物落在 `web/dist/`。

### 4.3 回到仓库根目录

```bash
cd ..
```

### 4.4 ⚠️ 这一步不能跳过

后端通过 `//go:embed all:web/dist`（见 [webui/webui.go](../webui/webui.go)）
把前端产物编进二进制。**`web/dist` 不存在或为空时 `go build` 直接编译失败** ——
是编译期错误，不是运行时提示。

由此带来两个后果：

- 干净的 clone 上，任何编译根包的命令都必须先做第四节
- `go test ./...` 同样会失败（它会把根包一起纳入编译）→ 见第七节

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

卡在 `database migration` 或直接退出，多半是 3.4 / 3.5 两步的密码不一致。

### 5.3 打开页面

浏览器访问 **http://localhost:3001**。

前端页面与 API 由同一个进程提供，不需要额外的 Nginx。

### 5.4 登录并立即改密

用户名 `root`，密码取 3.6 设的 `INITIAL_ROOT_PASSWORD`
（没设就去看 `root_initial_password.txt`）。**登录后立即改密码。**

---

## 六 改前端时开热更新

### 6.1 终端一：起后端

```bash
go run main.go
```

### 6.2 终端二：起 Vite 开发服务器

```bash
cd web && npm run dev
```

### 6.3 浏览器改开 :5173

**http://localhost:5173** —— 改前端代码即时生效。

`/api`、`/v1`、`/health` 由 Vite 代理到 :3001（见 [web/vite.config.ts](../web/vite.config.ts)）。

### 6.4 知道 :3001 上的页面不会跟着变

:3001 提供的是上次 `npm run build` 的产物，改前端不会同步过去。
要让 :3001 也用上新前端，回第四节重新构建。

---

## 七 跑测试和静态检查

### 7.1 跑测试

```bash
go test $(go list -e ./... | grep -vxF "$(go list -m)")
```

后半段的作用是**列出所有包、再把根包剔掉**。

### 7.2 跑静态检查

```bash
go vet $(go list -e ./... | grep -vxF "$(go list -m)")
```

### 7.3 为什么必须剔掉根包

`go test ./...` 会把根包一起纳入编译，而根包内嵌 `web/dist` ——
没构建前端时它编译不过，于是整条命令失败。

这不损失覆盖率 —— **根包里没有任何测试文件**，所有测试都在子包中
（`common` / `webui` / `middleware` / `controller` / `model`）。

> **需要测试的逻辑一律下沉到子包，不要让根包重新长出 `_test.go`。**

> 装了 make 的话：`make test` / `make vet` 就是上面两条命令。

---

## 八 出问题时的排查

### 8.1 现象：Windows 上报 `listen tcp :3001: bind: ...access permissions`

**这不是端口被占用**，是 Windows 把 3001 划进了系统保留区间
（Hyper-V / WSL2 / Docker Desktop 的 `winnat` 服务干的）。
`netstat` 查不到任何进程，但就是绑不上。

### 8.2 查被保留的端口段

```bash
netsh int ipv4 show excludedportrange protocol=tcp
```

### 8.3 查动态端口范围的起始值

```bash
netsh int ipv4 show dynamicport tcp
```

起始值是 **`1024`** 就是根因（Windows 默认应该是 `49152`）——
`winnat` 在 1024~15000 这段里随机保留，3001 很容易中招。

### 8.4 改回去（**用管理员权限的终端执行**）

```bash
netsh int ipv4 set dynamicport tcp start=49152 num=16384
netsh int ipv6 set dynamicport tcp start=49152 num=16384
```

执行完**重启**。

### 8.5 或者临时绕过：换个端口

改 `.env` 里的 `PORT`，同时改 [web/vite.config.ts](../web/vite.config.ts)
里三处 `target`。

### 8.6 现象：编译报找不到 `web/dist`

没做第四节。回到 4.1 构建前端。

### 8.7 现象：起应用时报数据库连接失败

按顺序排查这三条，它们长得都像「密码错」：

```bash
grep -n '^POSTGRES_USER=\|^POSTGRES_DB=\|^POSTGRES_PASSWORD=\|^SQL_DSN=' .env
```

1. 某个 `POSTGRES_*` 是空的，或排在了 `SQL_DSN=` 后面 —— 先回 3.5。
2. `SQL_DSN` 里变量的名字/大小写不对（只有 `[A-Z0-9_]` 会被展开）。
3. 改过 `POSTGRES_PASSWORD`，但数据卷是用旧密码初始化的 —— 密码只在数据卷
   首次初始化时写进数据库，之后改 `.env` 不会同步过去，需要进容器 `ALTER USER`
   或 `docker compose down -v` 重建（**会丢数据**）。

### 8.8 现象：`docker compose up -d postgres` 报 `POSTGRES_PASSWORD 必须设置`

没做第三节。`cp .env.example .env` 并填好 `POSTGRES_PASSWORD`。

---

## 九 环境变量速查（参考，不是步骤）

本地开发只需关心第三节填的那几项，其余保持默认。

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
