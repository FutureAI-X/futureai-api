# 部署

部署 **token-hub**：一个 Nginx 容器网关做 TLS 终止和域名分流，应用跑在它后面。

> 📖 **遇到不认识的词先查 [GLOSSARY.md](GLOSSARY.md)** ——
> 镜像/容器、反向代理、证书与 CA、Let's Encrypt、A 记录、SNI、cron 都有简短说明。
> 不用提前读，卡住了再查。

```
公网 ──80/443──> 网关(nginx) ──> token-hub:3001 ──> postgres
```

应用和数据库都不发布宿主机端口 —— 只有网关能访问它们。

> 这台机器以后还要放别的服务（同样接进网关）。做法见文末
> [以后要再加一个服务](#以后要再加一个服务)。

> **为什么这么设计**、**出问题怎么办**、**上线自检清单**、**升级/回滚/备份** —— 都在
> [NOTES.md](NOTES.md)。本文只讲照着敲的步骤。

## 步骤总览

| # | 在哪执行 | 做什么 |
|---|---|---|
| 0 | 服务器 | 建共享网络、放行端口 |
| 1 | **本机** | 构建镜像、把文件传上去 |
| 2 | 服务器 | 起网关 |
| 3 | 服务器 | 起 token-hub |
| 4 | 服务器 | 验证 |
| 5 | 服务器 | **有域名之后**换正式证书 |

第 5 步可以以后再做，不做也能用（浏览器会提示证书警告）。

**全文里的 `<user>@<server>`** 要换成你自己的登录信息，比如 `root@1.2.3.4`。
后面的命令都是在**服务器上**执行，除非标注了「本机」。

---

## 0. 服务器 · 一次性准备

**这一步在做什么**：查清服务器的 CPU 架构（后面构建镜像要用），
建一个给网关和应用互相通信用的网络。

```bash
uname -m
```
记下输出：`x86_64` → 后面用 `linux/amd64`；`aarch64` → 后面用 `linux/arm64`。

```bash
mkdir -p /opt/stacks

docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

- `mkdir` 建一个目录，后面网关和应用的配置文件都放这儿
- `docker network create` 建一个叫 `gateway-proxy` 的虚拟网络，网关靠它访问应用

> `--ip-range` 不能省。它保证网关的固定 IP `172.20.0.2` 不会被别的容器抢走 ——
> 网关停机时其他容器如果占了这个地址，网关就再也起不来了。

**还要做**：在云服务商的控制台（和服务器上的 `ufw`/`firewalld`，如果有）
放行 **22 / 80 / 443** 三个端口。80 和 443 是给网关用的。

---

## 1. 本机 · 构建并上传

**这一步在做什么**：把项目打包成一个 Docker 镜像，然后把镜像和配置文件传到服务器。
服务器上不需要源码、不需要 git、不需要装 Go 或 Node。

```bash
# 在本机（你的开发电脑），仓库根目录下执行
./deploy/build-image.sh linux/amd64          # 用第 0 步查到的架构
```

**预期看到**：最后打印 `完成，镜像包 14M`，并在仓库根目录生成
`token-hub-latest-amd64.tar.gz`。

```bash
# 把两个配置目录和镜像都传上去
scp -r deploy/gateway deploy/token-hub <user>@<server>:/opt/stacks/
scp token-hub-latest-amd64.tar.gz <user>@<server>:/tmp/
```

**预期看到**：`scp` 会打印传输进度，没有报错就是成功。

> 服务器上网络受限时，构建可能报 `failed to fetch anonymous token` ——
> 先手动 `docker pull node:22-alpine golang:1.27-alpine alpine:3.22` 再重试。
> 详见 [../docs/DOCKER.md](../docs/DOCKER.md)。

---

## 2. 服务器 · 起网关

**这一步在做什么**：让网关跑起来。它是全机器唯一占用 80/443 的东西，
按域名把请求分给后面的服务。

```bash
cd /opt/stacks/gateway
cp .env.example .env
vi .env
```

`vi` 会打开一个编辑器。要改的是这两行：

```
TOKEN_HUB_DOMAIN=token.example.com      ← 改成你自己的域名
ACME_EMAIL=admin@example.com            ← 改成你的邮箱（证书到期提醒会发这里）
```

> **域名还没买？** 先用默认的 `token.example.com` 别动，能跑通前面的流程，
> 只是后面签不了正式证书。等买了域名再回来改，然后 `docker compose up -d --force-recreate`。

改完保存退出（`vi` 的退出是依次按 `Esc`、`:wq`、回车），然后：

```bash
chmod +x scripts/*.sh
./scripts/self-signed.sh
```

**这一步在做什么**：生成一张自签证书。

**为什么必须先生成**：nginx 的证书文件不存在时**会直接启动失败** ——
不是某个域名不可用，是整个网关起不来。所以正式证书签发之前必须先放一张占位的。

**预期看到**：脚本自己会做检查，最后打印出证书的 `subject` 和 `DNS:` 域名。
如果只看到报错或者没打印这些，说明没生成成功，别往下走。

```bash
docker compose up -d
docker compose exec gateway nginx -t
```

**预期看到**：`nginx: configuration file /etc/nginx/nginx.conf test is successful`。

**这一步别跳过** —— 网关配置有 230 行，语法错误会让整个网关起不来。
先验证能省掉一轮瞎猜。

```bash
curl -k --resolve token.example.com:443:127.0.0.1 https://token.example.com/health
```

**预期看到**：`502 Bad Gateway`。**这是对的** —— 后面的应用还没起。

> **为什么命令这么长**：`--resolve token.example.com:443:127.0.0.1` 的意思是
> "这次请求把 `token.example.com` 当作 `127.0.0.1`"。这样 curl 发出的
> **SNI 和 Host 都是真实域名**，才能走到网关里对应的那个 server 块。
> 直接写 `https://127.0.0.1/` 会因为域名对不上被拒绝握手。

---

## 3. 服务器 · 起 token-hub

**这一步在做什么**：把镜像加载进来，配好密钥，启动应用和它的数据库。

```bash
gunzip -c /tmp/token-hub-latest-amd64.tar.gz | docker load
```

**预期看到**：`Loaded image: token-hub:latest`。

```bash
cd /opt/stacks/token-hub
cp .env.example .env
chmod 600 .env
```

下面三条命令各自**生成一串随机字符**，你需要把它们**复制下来填进 `.env`**：

```bash
openssl rand -hex 32      # 复制这行输出 -> 填到 JWT_SECRET
openssl rand -hex 32      # 复制这行输出 -> 填到 SECRET_KEY
openssl rand -hex 24      # 复制这行输出 -> 填到 POSTGRES_PASSWORD
```

```bash
vi .env
```

要填的四个地方：

```
POSTGRES_PASSWORD=        ← 粘上面第三个
JWT_SECRET=               ← 粘上面第一个
SECRET_KEY=               ← 粘上面第二个
INITIAL_ROOT_PASSWORD=    ← 自己设一个管理员初始密码，登录时用
```

> ⚠️ **`SECRET_KEY` 另外抄一份存到别处，并且以后永远不要改。**
> 它用来加密数据库里的供应商 API Key，改了之后已存的密钥就全部解不开、无法恢复。
> 这里的 `.env` 要和数据库一起备份。

保存退出，然后：

```bash
docker compose up -d
docker compose logs -f token-hub
```

**预期看到**（按 `Ctrl+C` 退出日志）：

```
using PostgreSQL as database
database migration started
Token Hub started on port 3001
```

如果卡在 `database migration` 或直接退出，多半是 `POSTGRES_PASSWORD` 填错了。

---

## 4. 服务器 · 验证

**这一步在做什么**：确认三层（网关 → 应用 → 数据库）真的打通了。

```bash
curl -k --resolve token.example.com:443:127.0.0.1 https://token.example.com/health
# 预期: {"status":"ok"}

curl -k -s --resolve token.example.com:443:127.0.0.1 https://token.example.com/ | head -3
# 预期: <!doctype html> ... 开头的一段 HTML

curl -k -s --resolve token.example.com:443:127.0.0.1 https://token.example.com/api/nope
# 预期: {"error":{"message":"Not found",...}}   —— JSON，不是 HTML
```

三条都对上，说明网关转发、应用响应、静态页面托管都正常。

### 最后一项：确认 `TRUSTED_PROXIES` 生效

**这项最容易漏，后果也最严重。** 它决定了应用能不能认出用户的真实 IP。

从外部（你自己的电脑）打开 `https://token.example.com`，故意输错几次密码，
然后在服务器上看：

```bash
docker compose logs token-hub | grep -i login
```

| 日志里的来源 IP | 含义 |
|---|---|
| 你自己的公网 IP | ✅ 正确 |
| `172.20.0.2` | ❌ 没生效 |

没生效的话，**所有用户会被登录限流当成同一个人** —— 任意几次失败就锁死所有人，
而现象看起来像"密码错了"，很难联想到限流。

修法：检查 `deploy/token-hub/.env` 里的 `GATEWAY_IP` 是不是 `172.20.0.2`，
以及 `deploy/gateway/docker-compose.yml` 里的 `ipv4_address` 是不是同一个值。

**最后**：浏览器登录 `https://token.example.com`，用户名 `root`、
密码是你在第 3 步设的 `INITIAL_ROOT_PASSWORD`。**登录后立即改密码**，
并把 `.env` 里 `INITIAL_ROOT_PASSWORD` 那行清空。

---

## 5. 服务器 · 换正式证书

> **这一步需要先有域名。** 域名还没买、解析还没配好就**跳过**，不影响使用 ——
> 只是浏览器会显示证书警告，点「继续前往」即可。等你有了域名再回来做。

### 前置条件

**1. 域名加了 A 记录，指向服务器公网 IP**

去你买域名的服务商控制台（阿里云、Cloudflare、Namecheap 等），找到 DNS 解析设置，
加一条记录：

| 类型 | 主机记录 | 值 |
|---|---|---|
| A | token | 你的服务器公网 IP |

"A 记录"就是"这个域名指向哪个 IP"。上面的 `token` 对应 `token.example.com`。

⚠️ 这里填的域名要和 `deploy/gateway/.env` 里**完全一致**，否则证书验证会失败。

**2. 解析已生效**

```bash
ping -c 1 token.example.com
```

**预期看到**：返回的 IP 是你的服务器公网 IP。刚配好可能要等几分钟到几小时（DNS 缓存）。
没生效就签不了证书。

**3. 80 端口能从公网访问**

防火墙和云安全组都要放行 —— Let's Encrypt 需要主动连回来验证。

### 签发

```bash
cd /opt/stacks/gateway
./scripts/certbot.sh issue
```

**它做了什么**：启动一个 certbot 容器，向 Let's Encrypt 申请证书。
Let's Encrypt 必须先确认"这个域名确实指向你这台机器"，
办法是它去访问 `http://你的域名/.well-known/acme-challenge/xxx`，
网关返回它要的内容。验证通过后证书下发，脚本自动复制到 `certs/` 并重载网关。

**预期看到**：一串 `Congratulations!` 之类的输出，最后是 `==> 证书已更新，重载网关`。

**如果报错**，最常见的是解析还没生效、或者 80 端口没放行。

### 验证

```bash
curl -I https://token.example.com/health
```

注意这次**没有 `-k` 了** —— 正式证书浏览器天然信任。应该返回 `HTTP/2 200`。

浏览器打开 `https://token.example.com`，地址栏应该是锁头图标、没有警告。

### 自动续期

Let's Encrypt 证书**有效期只有 90 天**，过期网站就打不开了。所以挂个定时任务：

```bash
crontab -e
```

会打开一个编辑器（第一次用会问选哪个，选 `nano` 最简单）。在文件末尾加一行：

```
17 3 * * * cd /opt/stacks/gateway && ./scripts/certbot.sh renew >> /var/log/certbot-renew.log 2>&1
```

保存退出（`nano` 是 `Ctrl+O` 回车，再 `Ctrl+X`）。

**这行怎么读**：开头五个字段表示"什么时候执行"：

```
17  3  *  *  *     cd /opt/stacks/gateway && ./scripts/certbot.sh renew
│   │  │  │  └── 星期几（* = 每天）
│   │  │  └───── 月  （* = 每月）
│   │  └──────── 日  （* = 每天）
│   └─────────── 时  （3 点）
└─────────────── 分  （17 分）
```

也就是**每天凌晨 3:17 检查一次**。`renew` 只在证书快到期时才真正续期，
其余时候什么也不做。末尾那串是把输出写进日志文件，方便以后回查。

**确认挂上了**：

```bash
crontab -l                                    # 应该能看到刚加的那行

cd /opt/stacks/gateway && ./scripts/certbot.sh renew   # 手动跑一次，不必等到凌晨
```

---

## 以后要再加一个服务

当前只部署了 token-hub。这台机器以后还要放别的服务，做法是**四处改动**：

**1. 那个服务的 compose 接进共享网络**（保留它自己的默认网络用来连数据库）：

```yaml
services:
  <服务名>:
    networks:
      - default    # 连它自己的数据库/Redis
      - proxy      # 被网关访问

networks:
  proxy:
    external: true
    name: gateway-proxy
```

**2. 删掉它的 `ports:`** —— 网关通过共享网络直连容器，发布到宿主机反而让它能被绕过网关访问。

**3. 网关加一个 server 块** —— 复制 [deploy/gateway/templates/default.conf.template](gateway/templates/default.conf.template)
里 token-hub 那个块，改四处：`server_name`、`access_log` 文件名、`set $xxx_up`（`服务名:端口`）、
以及流式服务需要的超时设置。**模板文件末尾有逐条说明**。

**4. `deploy/gateway/.env` 加域名，并同步改 `docker-compose.yml` 里的 `NGINX_ENVSUBST_FILTER`** ——
漏了后者的话，模板里新写的 `${NEW_DOMAIN}` 不会被替换，nginx 会当成字面量。

改完重新渲染并验证：

```bash
cd /opt/stacks/gateway
docker compose up -d --force-recreate
docker compose exec gateway nginx -t
```

> ⚠️ 如果那个服务是**流式**返回的（一次回答持续几十秒到几分钟），
> 必须给它加这两行：
>
> ```nginx
> proxy_read_timeout 600s;
> proxy_buffering off;
> ```
>
> 漏了的话长回答会在中途被**静默截断** —— 连接是正常断开的，日志里看不出任何异常，
> 现象只是"回答到一半卡住"。另外证书要重新签（SAN 里得有新域名）。

---

## 接下来

- 上线前逐项自检 → [NOTES.md](NOTES.md#上线自检清单)
- 升级 / 回滚 / 备份 → [NOTES.md](NOTES.md#升级)
- 出问题了 → [NOTES.md](NOTES.md#排查表)
