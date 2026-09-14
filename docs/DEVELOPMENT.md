# 本地开发

日常改代码的流程。用 Docker 跑打包后的镜像见 [DOCKER.md](DOCKER.md)。

## 前置条件

| 需要 | 版本 | 用来做什么 |
|---|---|---|
| Go | 1.27+ | `go.mod` 里写的是 `go 1.27.0` |
| Node.js | 20+ | 构建前端 |
| Docker | 近期版本即可 | **只用来跑 PostgreSQL**，装了 Docker Desktop 就行 |

> 不想用 Docker 跑数据库也行 —— 本机装个 PostgreSQL 15，把 `.env` 里的
> `SQL_DSN` 指过去即可。

> 仓库里有个 Makefile，`make web` / `make run` / `make test` 是下面这些命令的快捷方式。
> 但 **Windows 的 Git Bash 默认没有 make**，所以本文档一律用裸命令 ——
> 复制粘贴到哪儿都能跑。

---

## 1. 起数据库

```bash
docker compose up -d postgres
```

注意带了服务名。不加的话会连应用容器一起起 —— 那是 [DOCKER.md](DOCKER.md) 的用法。

```bash
docker compose down               # 停止，数据保留在卷里
docker compose down -v            # 连数据一起删，慎用
docker compose logs -f postgres   # 看日志
```

数据库只绑定 `127.0.0.1:5432`，局域网里的其他机器连不上。

---

## 2. 配置 .env

```bash
cp .env.example .env
```

填三个值，其余保持默认：

```bash
openssl rand -hex 32      # -> JWT_SECRET
openssl rand -hex 32      # -> SECRET_KEY
openssl rand -hex 24      # -> POSTGRES_PASSWORD
                          #    还要同步替换 SQL_DSN 里的 CHANGE_ME
```

> ⚠️ `SECRET_KEY` 用来加密数据库里的供应商 API Key。
> **一旦用于加密数据后不可更改** —— 改了已存的密钥全部解不开且无法恢复。

---

## 3. 构建前端

```bash
cd web && npm ci && npm run build && cd ..
```

### ⚠️ 这一步不能跳过

后端通过 `//go:embed all:web/dist` 把前端产物编进二进制（见 [webui/](../webui/)）。
**该目录不存在或为空时 `go build` 会直接编译失败** —— 是编译期错误，不是运行时提示。

由此带来两个后果：

- 干净的 clone 上，任何编译根包的命令都必须先构建前端
- `go test ./...` 同样会失败（它会把根包一起纳入编译）→ 见下面「测试」一节

---

## 4. 启动后端

```bash
go run main.go
```

访问 **http://localhost:3001** —— 前端页面与 API 由同一个进程提供，
不需要额外的 Nginx。

首次启动且 `users` 表为空时会自动创建 `root` 用户，密码取 `.env` 里的
`INITIAL_ROOT_PASSWORD`。**登录后立即改密。**

---

## 5. 改前端时用热更新

```bash
cd web && npm run dev
```

开发服务器在 **http://localhost:5173**，并把 `/api`、`/v1`、`/health` 代理到后端。
改前端不必重新编译后端 —— 此时后端提供的仍是上次 `npm run build` 的产物。

### 日常怎么开

一个终端跑 `go run main.go`，另一个终端跑 `cd web && npm run dev`，
浏览器开 **:5173**（有热更新）。

---

## 测试

```bash
go test $(go list -e ./... | grep -vxF "$(go list -m)")
```

后半段的作用是**列出所有包、再把根包剔掉**。

> **为什么要剔根包**：根包 `main` 内嵌 `web/dist`，没构建前端时它编译不过，
> 而 `go test ./...` 会把根包一起纳入编译，于是整条命令失败。
>
> 这不损失覆盖率 —— 根包里**没有**任何测试文件，所有测试都在子包中
> （`common` / `webui` / `middleware` / `controller` / `model`）。
> **需要测试的逻辑一律下沉到子包，不要让根包重新长出 `_test.go`。**
>
> `go vet` 同理，把命令里的 `go test` 换成 `go vet` 即可。

---

## 常见问题

### Windows：`listen tcp :3001: bind: ...access permissions`

**不是端口被占用**，是 Windows 把 3001 划进了系统保留区间（Hyper-V / WSL2 /
Docker Desktop 的 `winnat` 服务干的）。`netstat` 查不到任何进程，但就是绑不上。

先确认：

```bash
netsh int ipv4 show excludedportrange protocol=tcp
netsh int ipv4 show dynamicport tcp
```

如果动态端口范围的**起始值是 `1024`**（Windows 默认应该是 `49152`），那就是根因 ——
`winnat` 在 1024~15000 这段里随机保留，3001 很容易中招。**用管理员执行后重启**：

```bash
netsh int ipv4 set dynamicport tcp start=49152 num=16384
netsh int ipv6 set dynamicport tcp start=49152 num=16384
```

临时绕过：`.env` 里换个端口，同时改 [web/vite.config.ts](../web/vite.config.ts)
里三处 `target`。

---

## 环境变量

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
| `POSTGRES_PASSWORD` | 本地开发库的密码（仅 docker compose 用） | 必须设置 |

本地开发时 `TRUSTED_PROXIES` **留空**即可 —— 前面没有反向代理。
（填 `127.0.0.1/32` 也不会出错，只是没有意义：那个值是在告诉应用
「去相信一个并不存在的代理写的 `X-Forwarded-For`」。）
