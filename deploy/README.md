# 部署

部署 **futureai-api**：一个 Nginx 容器做网关，负责 TLS 终止和域名分流，
应用跑在它后面。

```
公网 ──80/443──> 网关(nginx) ──> futureai-api:3001 ──> postgres
```

应用和数据库都**不发布宿主机端口** —— 只有网关能访问它们。

**全文约定**

| 写法 | 换成 |
|---|---|
| `<user>@<server>` | 你的登录信息，如 `root@1.2.3.4` |
| `20260915225600` | 第 2.2 步打印出来的实际标签 |
| 标题里的 `服务器` / `本机` | 这一节在哪台机器上执行 |

**相关文档**

- 遇到不认识的词（镜像、反向代理、证书与 CA、A 记录、SNI、cron）→ [GLOSSARY.md](GLOSSARY.md)
- 为什么这么设计、上线自检清单、升级/回滚/备份、排查表 → [NOTES.md](NOTES.md)
- 本文只讲照着敲的步骤。

---

## 一 服务器：一次性准备

### 1.1 查 CPU 架构

```bash
uname -m
```

`x86_64` → 后面用 `linux/amd64`；`aarch64` → 后面用 `linux/arm64`。
**记下来**，第 2.1 步要用。

### 1.2 建配置目录

```bash
mkdir -p /opt/stacks
```

后面网关和应用的配置文件都放这儿。

### 1.3 建共享网络

```bash
docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

网关靠这个网络访问应用。

> ⚠️ `--ip-range` **不能省**。它保证网关的固定 IP `172.20.0.2` 不会被别的容器抢走 ——
> 网关停机时其他容器如果占了这个地址，网关就再也起不来了。

### 1.4 放行 22 / 80 / 443

在云服务商的控制台（和服务器上的 `ufw` / `firewalld`，如果有）都放行这三个端口。
80 和 443 是给网关用的。

---

## 二 本机：构建镜像并上传

> 服务器上不需要源码、不需要 git、不需要装 Go 或 Node。

### 2.1 构建镜像

```bash
bash deploy/build-image.sh linux/amd64
```

把 `linux/amd64` 换成第 1.1 步查到的架构。

> 用 `bash` 前缀而不是 `./`：脚本的执行位依赖 git 里的文件模式，
> 在部分环境（旧 clone、从压缩包解出的副本）可能没带上，
> 直接 `./` 会报 `Permission denied`。`bash <脚本>` 没有这个问题。

### 2.2 记下打印出来的标签

**预期看到**：最后一行形如

```
完成，镜像包 14M，标签 20260915225600。
```

标签默认取构建时刻（年月日时分秒），所以**你实际看到的数字和这里不会一样**。
本手册后面统一拿 `20260915225600` 当占位符。第 4.1 步和第 4.10 步还要用它，
**先记下来** —— 换了机器就翻不回这一步了。

### 2.3 上传网关配置

```bash
scp deploy/gateway/docker-compose.yml deploy/gateway/.env.example \
    <user>@<server>:/opt/stacks/gateway/
```

> ⚠️ 刻意逐个列出要传的文件，而不是 `scp -r deploy/gateway`：
> 后者会把本机的 `certs/`（含私钥）、`logs/`、`.env` 一起带上服务器。
> 后果不小：本机那张占位证书传过去之后，第 3.5 步的 `self-signed.sh`
> 会因为「证书已存在」直接跳过并返回 0，你会以为这步失败了、
> 或者更糟 —— 真的用了那张域名对不上的证书。

### 2.4 上传网关的目录

```bash
scp -r deploy/gateway/templates deploy/gateway/snippets deploy/gateway/scripts \
    <user>@<server>:/opt/stacks/gateway/
```

### 2.5 上传应用配置

```bash
scp deploy/futureai-api/docker-compose.yml deploy/futureai-api/.env.example \
    <user>@<server>:/opt/stacks/futureai-api/
```

### 2.6 上传备份脚本

```bash
scp deploy/backup.sh deploy/restore.sh <user>@<server>:/opt/stacks/futureai-api/
```

### 2.7 上传镜像包

```bash
scp futureai-api-20260915225600-amd64.tar.gz <user>@<server>:/tmp/
```

**预期看到**：`scp` 会打印传输进度，没有报错就是成功。

### 2.8 构建报 `failed to fetch anonymous token` 怎么办

在**本机**先执行这三条，再重新跑 2.1：

```bash
docker pull node:22-alpine
```

```bash
docker pull golang:1.27-alpine
```

```bash
docker pull alpine:3.22
```

详见 [../docs/DOCKER.md](../docs/DOCKER.md)。

---

## 三 服务器：起网关

> 网关是全机器唯一占用 80/443 的东西，按域名把请求分给后面的服务。

### 3.1 进入目录并复制配置

```bash
cd /opt/stacks/gateway && cp .env.example .env
```

### 3.2 填域名

```bash
vi .env
```

把 `FUTUREAI_API_DOMAIN=` 改成你自己的域名。

> **域名还没买？** 先留着默认的 `futureai.example.com` 别动，能跑通前面的流程，
> 只是后面签不了正式证书。等买了域名再回来改，然后
> `docker compose up -d --force-recreate`。

### 3.3 填邮箱

同一个文件里的 `ACME_EMAIL=` 改成你的邮箱（证书到期提醒会发这里）。

### 3.4 保存退出

`vi` 里依次按 `Esc`、`:wq`、回车。

### 3.5 生成自签证书

```bash
chmod +x scripts/*.sh && ./scripts/self-signed.sh
```

**为什么必须先有一张**：nginx 的证书文件不存在时**会直接启动失败** ——
不是某个域名不可用，是整个网关起不来。所以正式证书签发之前必须先放一张占位的。

**预期看到**：脚本会自检，最后打印出证书的 `subject` 和 `DNS:` 域名。
只看到报错、或没打印这些，就是没生成成功，别往下走。

### 3.6 起网关

```bash
docker compose up -d
```

### 3.7 验证网关配置

```bash
docker compose exec gateway nginx -t
```

**预期看到**：`nginx: configuration file /etc/nginx/nginx.conf test is successful`。

**这一步别跳过** —— 网关配置有两百多行，语法错误会让整个网关起不来。
先验证能省掉一轮瞎猜。

### 3.8 用 curl 确认网关活着

```bash
curl -k --resolve futureai.example.com:443:127.0.0.1 https://futureai.example.com/health
```

**预期看到 `502 Bad Gateway`。这是对的** —— 后面的应用还没起。

> **为什么命令这么长**：`--resolve futureai.example.com:443:127.0.0.1` 的意思是
> "这次请求把 `futureai.example.com` 当作 `127.0.0.1`"。这样 curl 发出的
> **SNI 和 Host 都是真实域名**，才能走到网关里对应的那个 server 块。
> 直接写 `https://127.0.0.1/` 会因为域名对不上被拒绝握手。

---

## 四 服务器：起 futureai-api

### 4.1 加载镜像

```bash
gunzip -c /tmp/futureai-api-20260915225600-amd64.tar.gz | docker load
```

`20260915225600` 换成第 2.2 步记下的那个标签。

**预期看到**：`Loaded image: futureai-api:20260915225600` 与
`Loaded image: futureai-api:latest` —— 同一个镜像带这两个标签。

### 4.2 进入目录

```bash
cd /opt/stacks/futureai-api
```

### 4.3 复制配置并收紧权限

```bash
cp .env.example .env && chmod 600 .env
```

### 4.4 建数据卷

```bash
docker volume create futureai-api-prod-data
```

compose 里把它声明成 `external`（防 `docker compose down -v` 误删生产库），
因此 compose 不会替你创建，必须先手工建好 —— 否则 `up` 时报
`external volume ... not found`。

### 4.5 给备份脚本加执行位

```bash
chmod +x backup.sh restore.sh
```

如果 scp 时没带上执行位才有必要，加一次无副作用。

### 4.6 生成 JWT_SECRET

```bash
openssl rand -hex 32
```

复制这行输出，填到 `.env` 的 `JWT_SECRET=`。

### 4.7 生成 SECRET_KEY

```bash
openssl rand -hex 32
```

复制这行输出，填到 `.env` 的 `SECRET_KEY=`。

> ⚠️ **另外抄一份存到别处，并且以后永远不要改。**
> 它用来加密数据库里的供应商 API Key，改了之后已存的密钥就全部解不开、无法恢复。
> 这里的 `.env` 要和数据库一起备份。

### 4.8 生成数据库密码

```bash
openssl rand -hex 24
```

复制这行输出，填到 `.env` 的 `POSTGRES_PASSWORD=`。

### 4.9 打开配置

```bash
vi .env
```

### 4.10 填这五个值

```
POSTGRES_PASSWORD=        ← 粘 4.8 的输出
JWT_SECRET=               ← 粘 4.6 的输出
SECRET_KEY=               ← 粘 4.7 的输出
INITIAL_ROOT_PASSWORD=    ← 自己设一个管理员初始密码，登录时用
FUTUREAI_API_TAG=         ← 第 2.2 步的标签，形如 20260915225600
```

> **`FUTUREAI_API_TAG` 填错会起不来**：它决定跑哪个版本的镜像，要和第 2.2 步的标签
> **一字不差**，否则 `docker compose up -d` 会报 `image "futureai-api:xxx" not found`。
> 第一次部署想先跑通流程，也可以填 `latest`（打镜像时顺带打了这个标签）；
> 但升级和回滚要填具体标签，理由见本机仓库的 [NOTES.md](NOTES.md) —— 它不在服务器上。

### 4.11 保存退出

`vi` 里依次按 `Esc`、`:wq`、回车。

### 4.12 起应用和数据库

```bash
docker compose up -d
```

### 4.13 看启动日志

```bash
docker compose logs -f futureai-api
```

**预期看到**（按 `Ctrl+C` 退出）：

```
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

卡在 `database migration` 或直接退出，多半是 `POSTGRES_PASSWORD` 填错了。

---

## 五 服务器：验证三层是否打通

### 5.1 应用健康检查

```bash
curl -k --resolve futureai.example.com:443:127.0.0.1 https://futureai.example.com/health
```

**预期**：`{"status":"ok"}`

### 5.2 首页有 HTML

```bash
curl -k -s --resolve futureai.example.com:443:127.0.0.1 https://futureai.example.com/ | head -3
```

**预期**：`<!doctype html>` 开头的一段 HTML。

### 5.3 API 路径的 404 是 JSON

```bash
curl -k -s --resolve futureai.example.com:443:127.0.0.1 https://futureai.example.com/api/nope
```

**预期**：`{"error":{"message":"Not found",...}}`

三条都对上，说明网关转发、应用响应、静态页面托管都正常。

### 5.4 确认 TRUSTED_PROXIES 生效

**这项最容易漏，后果也最严重** —— 它决定应用能不能认出用户的真实 IP。

从外部（你自己的电脑）打开 `https://futureai.example.com`，
故意输错几次密码，然后回服务器上看：

```bash
docker compose logs futureai-api | grep -i login
```

| 日志里的来源 IP | 含义 |
|---|---|
| 你自己的公网 IP | ✅ 正确 |
| `172.20.0.2` | ❌ 没生效 |

没生效的话，**所有用户会被登录限流当成同一个人** —— 任意几次失败就锁死所有人，
而现象看起来像「密码错了」，很难联想到限流。

### 5.5 修法：核对两处值一致

`deploy/futureai-api/.env` 里的 `GATEWAY_IP` 与
`deploy/gateway/docker-compose.yml` 里的 `ipv4_address`，两处都应该是 `172.20.0.2`。

### 5.6 登录并立即改密

浏览器打开 `https://futureai.example.com`，用户名 `root`，
密码是第 4.10 步设的 `INITIAL_ROOT_PASSWORD`。

### 5.7 清空 .env 里的初始密码

```bash
vi .env
```

把 `INITIAL_ROOT_PASSWORD=` 那行清空，保存退出。

---

## 六 服务器：有域名之后换正式证书

> 域名还没买、解析还没配好就**跳过整节**，不影响使用 ——
> 只是浏览器会显示证书警告，点「继续前往」即可。等你有了域名再回来做。

### 6.1 加 A 记录

去你买域名的服务商控制台（阿里云、Cloudflare、Namecheap 等），
找到 DNS 解析设置，加一条记录：

| 类型 | 主机记录 | 值 |
|---|---|---|
| A | futureai | 你的服务器公网 IP |

「A 记录」就是「这个域名指向哪个 IP」。上面的 `futureai` 对应 `futureai.example.com`。

> ⚠️ 这里填的域名要和 `deploy/gateway/.env` 里**完全一致**，否则证书验证会失败。

### 6.2 确认解析已生效

```bash
ping -c 1 futureai.example.com
```

**预期看到**：返回的 IP 是你的服务器公网 IP。
刚配好可能要等几分钟到几小时（DNS 缓存）。没生效就签不了证书。

### 6.3 确认 80 端口能从公网访问

防火墙和云安全组都要放行 —— Let's Encrypt 需要主动连回来验证。

### 6.4 签发证书

```bash
cd /opt/stacks/gateway && ./scripts/certbot.sh issue
```

**它做了什么**：启动一个 certbot 容器向 Let's Encrypt 申请证书。
Let's Encrypt 必须先确认「这个域名确实指向你这台机器」，办法是它去访问
`http://你的域名/.well-known/acme-challenge/xxx`，网关返回它要的内容。
验证通过后证书下发，脚本自动复制到 `certs/` 并重载网关。

**预期看到**：一串 `Congratulations!` 之类的输出，最后是 `==> 证书已更新，重载网关`。

**如果报错**：最常见的是解析还没生效、或者 80 端口没放行。

### 6.5 验证（这次不用 `-k`）

```bash
curl -I https://futureai.example.com/health
```

正式证书浏览器天然信任，所以去掉 `-k`。**预期看到** `HTTP/2 200`。

浏览器打开应该是锁头图标、没有警告。

### 6.6 打开定时任务编辑器

```bash
sudo crontab -e
```

> **必须用 `sudo`**：日志写在 `/var/log/` 下，普通用户的 crontab 没有写权限，
> 会静默失败 —— 直到某天证书过期、网站打不开才发现。

第一次用会问选哪个编辑器，选 `nano` 最简单。

### 6.7 在文件末尾加两行

```
17 3 * * * cd /opt/stacks/gateway && ./scripts/certbot.sh renew >> /var/log/certbot-renew.log 2>&1
30 3 * * * /opt/stacks/futureai-api/backup.sh >> /var/log/futureai-api-backup.log 2>&1
```

第一行：**每天凌晨 3:17** 检查一次证书续期。Let's Encrypt 证书**有效期只有 90 天**，
过期网站就打不开。`renew` 只在证书快到期时才真正续期，其余时候什么也不做。

第二行：每天凌晨 3:30 备份数据库。

末尾那串 `>> /var/log/... 2>&1` 是把输出写进日志文件，方便以后回查。

> 五个字段的含义是 `分 时 日 月 星期`，`*` 表示「都行」。

### 6.8 保存退出

`nano` 是 `Ctrl+O` 回车，再 `Ctrl+X`。

### 6.9 确认挂上了

```bash
crontab -l
```

应该能看到刚加的那两行。

### 6.10 手动跑一次续期（不必等到凌晨）

```bash
cd /opt/stacks/gateway && ./scripts/certbot.sh renew
```

---

## 七 服务器：以后要再加一个服务

当前只部署了 futureai-api。这台机器以后还要放别的服务，做法是四处改动。

### 7.1 把那个服务接进共享网络

在它自己的 `docker-compose.yml` 里加（保留它自己的默认网络用来连数据库）：

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

### 7.2 删掉它的 `ports:`

网关通过共享网络直连容器。发布到宿主机反而让它能被绕过网关访问。

### 7.3 网关加一个 server 块

复制 [gateway/templates/default.conf.template](gateway/templates/default.conf.template)
里 futureai-api 那个块，改四处：`server_name`、`access_log` 文件名、
`set $xxx_up`（`服务名:端口`）、以及流式服务需要的超时设置。
**模板文件末尾有逐条说明。**

### 7.4 给流式服务加超时（不是流式就跳过）

```nginx
proxy_read_timeout 600s;
proxy_buffering off;
```

> ⚠️ 那个服务如果是**流式**返回的（一次回答持续几十秒到几分钟），必须加这两行。
> 漏了的话长回答会在中途被**静默截断** —— 连接是正常断开的，日志里看不出任何异常，
> 现象只是「回答到一半卡住」。另外证书要重新签（SAN 里得有新域名）。

### 7.5 在网关 .env 加域名，并同步改 filter

`deploy/gateway/.env` 加域名，同时改 `docker-compose.yml` 里的
`NGINX_ENVSUBST_FILTER`。

漏了后者的话，模板里新写的 `${NEW_DOMAIN}` 不会被替换，nginx 会当成字面量。

### 7.6 重新渲染并验证

```bash
cd /opt/stacks/gateway && docker compose up -d --force-recreate && docker compose exec gateway nginx -t
```

---

## 接下来

- 上线前逐项自检 → [NOTES.md](NOTES.md#上线自检清单)
- 升级 / 回滚 / 备份 → [NOTES.md](NOTES.md#升级)
- 出问题了 → [NOTES.md](NOTES.md#排查表)
