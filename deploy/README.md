# 部署

部署 **futureai-api**：一个 Nginx 容器做网关，负责 TLS 终止和域名分流，应用运行在它后面。

## 1 开始之前

### 1.1 这份文档做什么

```
公网 ──80/443──> 网关(nginx) ──> futureai-api:3001 ──> postgres
```

应用和数据库都**不发布宿主机端口**，只有网关能访问它们。

本文只讲照着敲的步骤。为什么这么设计，见 [NOTES.md](NOTES.md#1-为什么是这套结构)。

### 1.2 全文约定

文中出现的占位写法及其含义如下。

| 写法 | 换成 |
|---|---|
| `<user>@<server>` | 你的登录信息，如 `root@1.2.3.4` |
| `20260915225600` | 第 3.2 步打印出来的实际标签 |
| 章节标题里的 `服务器` / `本机` | 这一章在哪台机器上执行 |

### 1.3 相关文档

本文之外的内容按下面的对应关系查阅。

| 我想… | 看 |
|---|---|
| 搞懂不认识的词（镜像、反向代理、证书与 CA、A 记录、SNI、cron） | [GLOSSARY.md](GLOSSARY.md) |
| 知道为什么这么设计 | [NOTES.md](NOTES.md#1-为什么是这套结构) |
| 上线前逐项自检 | [NOTES.md](NOTES.md#5-上线自检清单) |
| 升级 / 回滚 / 备份 | [NOTES.md](NOTES.md#6-升级) |
| 出问题了 | [NOTES.md](NOTES.md#10-排查表) |

### 1.4 部署路线

各章的执行机器与内容如下。

| 章 | 在哪台机器 | 做什么 |
|---|---|---|
| 1 | — | 开始之前（你正在读） |
| 2 | 服务器 | 一次性准备：建目录、建网络、放行端口 |
| 3 | 本机 | 构建镜像并上传到服务器 |
| 4 | 服务器 | 起网关 |
| 5 | 服务器 | 起应用和数据库 |
| 6 | 服务器 | 验证三层是否打通 |
| 7 | 服务器 | 首次登录并改密 |
| 8 | 服务器 | 有域名后换正式证书 |
| 9 | 服务器 | 挂定时任务（证书续期、备份、日志清理） |
| 10 | 服务器 | （以后）再往这台机器上加服务 |

**1 到 7 做完就是一个可用的部署**（自签证书，浏览器会警告）。第 8 章需要域名，没配好解析可以稍后再做；第 9 章不依赖域名，应当完成。第 10 章现在用不到。

## 2 服务器：一次性准备

### 2.1 查 CPU 架构

```bash
uname -m
```

`x86_64` → 后面用 `linux/amd64`；`aarch64` → 后面用 `linux/arm64`。**记下来**，第 3.1 步要用。

### 2.2 建配置目录

```bash
mkdir -p /opt/stacks
```

网关和应用的配置都放在这个目录下。

### 2.3 建共享网络

```bash
docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

网关靠这个网络访问应用。

> **注意**：`--ip-range` 不可省略。它保证网关的固定 IP `172.20.0.2` 不被别的容器占用；网关停机时其他容器如果占了这个地址，网关就再也起不来了。

> 报 `already exists` 说明之前建过，直接跳到下一节即可。

### 2.4 放行 22 / 80 / 443

在云服务商的控制台（以及服务器上的 `ufw` / `firewalld`，如果有）都放行这三个端口。80 和 443 是给网关用的。

## 3 本机：构建镜像并上传

> 服务器上不需要源码、不需要 git、不需要安装 Go 或 Node。

### 3.1 构建镜像

```bash
bash deploy/build-image.sh linux/amd64
```

把 `linux/amd64` 换成第 2.1 步查到的架构。

> 用 `bash` 前缀而不是 `./`：脚本的执行位依赖 git 里的文件模式，在部分环境（旧 clone、从压缩包解出的副本）可能没带上，直接 `./` 会报 `Permission denied`。`bash <脚本>` 没有这个问题。

**若报 `failed to fetch anonymous token`**，说明网络受限。先手动拉取三个基础镜像：

```bash
docker pull node:22-alpine
```

```bash
docker pull golang:1.27-alpine
```

```bash
docker pull alpine:3.22
```

再重新执行本步。以后修改 Dockerfile 中的基础镜像 tag，也要先拉取一次。理由见 [../docs/DOCKER.md](../docs/DOCKER.md)。

### 3.2 记下镜像标签

**预期看到**：最后一行形如

```
完成，镜像包 14M，标签 20260915225600。
```

标签默认取构建时刻（年月日时分秒），所以**你实际看到的数字和这里不会一样**。本文后面统一拿 `20260915225600` 当占位符。第 5.1 步和第 5.9 步还要用它，**先记下来**，换了机器就翻不回这一步了。

### 3.3 上传网关配置

```bash
scp deploy/gateway/docker-compose.yml deploy/gateway/.env.example <user>@<server>:/opt/stacks/gateway/
```

### 3.4 上传网关的模板、片段与脚本

```bash
scp -r deploy/gateway/templates deploy/gateway/snippets deploy/gateway/scripts <user>@<server>:/opt/stacks/gateway/
```

> **注意**：3.3 和 3.4 刻意逐个列出要传的文件，而不是 `scp -r deploy/gateway`。后者会把本机的 `certs/`（含私钥）、`logs/`、`.env` 一起带上服务器。后果不小：本机那张占位证书传过去之后，第 4.4 步的 `self-signed.sh` 会因为「证书已存在」直接跳过并返回 0，你会以为这步失败了，或者更糟——真的用了那张域名对不上的证书。

### 3.5 上传应用配置

```bash
scp deploy/futureai-api/docker-compose.yml deploy/futureai-api/.env.example <user>@<server>:/opt/stacks/futureai-api/
```

### 3.6 上传备份脚本

```bash
scp deploy/backup.sh deploy/restore.sh <user>@<server>:/opt/stacks/futureai-api/
```

### 3.7 上传镜像包

```bash
scp futureai-api-20260915225600-amd64.tar.gz <user>@<server>:/tmp/
```

`20260915225600` 换成第 3.2 步记下的标签。`scp` 会打印传输进度，没有报错就是成功。

## 4 服务器：起网关

> 网关是全机器唯一占用 80/443 的东西，按域名把请求分给后面的服务。

### 4.1 复制网关配置

进入网关配置目录，本章以下命令均在此目录执行：

```bash
cd /opt/stacks/gateway
```

复制配置：

```bash
cp .env.example .env
```

### 4.2 填域名

```bash
vi .env
```

确认 `FUTUREAI_API_DOMAIN=` 是 `futureaiapi.com`。

`.env.example` 里的默认值就是它，**通常不用改**。只有换域名部署时才需要改，改完还必须重渲染网关，否则 nginx 仍用旧域名：

```bash
docker compose up -d --force-recreate
```

### 4.3 填邮箱

同一个文件里的 `ACME_EMAIL=` 默认值也是本项目在用的，**通常不用改**。

它只用于 ACME 注册（证书与账号相关的事务联系）。**不要指望它做到期提醒**：Let's Encrypt 已于 2025 年 6 月停发证书到期通知邮件，续期只能靠第 9.1 步那条 cron。

`vi` 里依次按 `Esc`、`:wq`、回车保存退出。

### 4.4 生成自签证书

```bash
chmod +x scripts/*.sh
```

```bash
./scripts/self-signed.sh
```

**为什么必须先有一张**：nginx 的证书文件不存在时会**直接启动失败**，不是某个域名不可用，而是整个网关起不来。所以正式证书签发之前必须先放一张占位的。

**预期看到**：脚本会自检，最后打印出证书的 `subject` 和 `DNS:` 域名。只看到报错、或没打印这些，就是没生成成功，不要往下走。

### 4.5 起网关

```bash
docker compose up -d
```

### 4.6 验证网关配置

```bash
docker compose exec gateway nginx -t
```

**预期看到**：`nginx: configuration file /etc/nginx/nginx.conf test is successful`。

**这步不要跳过**：网关配置有两百多行，语法错误会让整个网关起不来，先验证能省掉一轮盲猜。

### 4.7 确认网关活着

```bash
curl -k --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/health
```

**预期看到 `502 Bad Gateway`，这是对的**，后面的应用还没起。

> **注意**：命令中的 `--resolve futureaiapi.com:443:127.0.0.1` 表示「这次请求把 `futureaiapi.com` 当作 `127.0.0.1`」，这样 curl 发出的 **SNI 和 Host 都是真实域名**，才能走到网关里对应的那个 `server` 块。直接写 `https://127.0.0.1/` 会因为域名对不上被拒绝握手。原理见 [GLOSSARY.md 第 6 章](GLOSSARY.md#6-sni)。

## 5 服务器：起 futureai-api

### 5.1 加载镜像

```bash
gunzip -c /tmp/futureai-api-20260915225600-amd64.tar.gz | docker load
```

`20260915225600` 换成第 3.2 步记下的那个标签。

**预期看到**：`Loaded image: futureai-api:20260915225600` 与 `Loaded image: futureai-api:latest`，同一个镜像带这两个标签。

### 5.2 进入应用目录

```bash
cd /opt/stacks/futureai-api
```

### 5.3 复制配置并收紧权限

```bash
cp .env.example .env
```

```bash
chmod 600 .env
```

`.env` 里是密钥，权限收紧到只有属主可读写。

### 5.4 给备份脚本加执行位

```bash
chmod +x backup.sh restore.sh
```

scp 没带上执行位时才有必要，加一次无副作用。

### 5.5 建数据卷

```bash
docker volume create futureai-api-prod-data
```

**必须先手工建**：compose 里把它声明成 `external`（防 `docker compose down -v` 误删生产库），因此 compose 不会替你创建，否则 `up` 时报 `external volume ... not found`。

### 5.6 生成 JWT_SECRET

```bash
openssl rand -hex 32
```

复制输出，第 5.9 步粘到 `.env` 的 `JWT_SECRET=`。

它用来签发与校验登录 Token。换掉会让所有已签发的 Token 立即失效。

### 5.7 生成 SECRET_KEY

```bash
openssl rand -hex 32
```

复制输出，第 5.9 步粘到 `.env` 的 `SECRET_KEY=`。

> **注意**：该项一旦用于加密数据后不可更改，且必须另存一份到密码管理器。它用来加密数据库里的供应商 API Key，改了之后已存的密钥全部解不开、无法恢复。这正是第 9.1 步的备份要连 `.env` 一起备的原因。

### 5.8 生成数据库密码

```bash
openssl rand -hex 24
```

复制输出，第 5.9 步粘到 `.env` 的 `POSTGRES_PASSWORD=`。

只能用 hex 或 base64url 这类不含 `@ : / #` 的字符，因为它会被原样拼进连接串。`openssl rand -hex` 生成的就是安全的。

### 5.9 填 .env 的五个值

```bash
vi .env
```

需要填写的五处如下。

```
POSTGRES_PASSWORD=        ← 粘 5.8 的输出
JWT_SECRET=               ← 粘 5.6 的输出
SECRET_KEY=               ← 粘 5.7 的输出
INITIAL_ROOT_PASSWORD=    ← 自己设一个管理员初始密码，登录时用
FUTUREAI_API_TAG=         ← 第 3.2 步的标签，形如 20260915225600
```

`vi` 里依次按 `Esc`、`:wq`、回车保存退出。

> **注意**：`JWT_SECRET` 或 `SECRET_KEY` 留空、过短，服务会拒绝启动，这是刻意的 fail-closed。

> **注意**：`FUTUREAI_API_TAG` 要和第 3.2 步的标签一字不差，否则 `docker compose up -d` 会报 `image "futureai-api:xxx" not found`。

> **注意**：不要图省事填 `latest`。打镜像时确实顺带打了这个标签，但它会在每次 `docker load` 时**静默指向新版本**：改了标签 `docker compose up -d` 未必重建容器，服务器上跑的到底是哪一版就说不清了，回滚也无从下手（见 [NOTES.md 第 7 章](NOTES.md#7-回滚)）。

### 5.10 起应用和数据库

```bash
docker compose up -d
```

### 5.11 看启动日志

```bash
docker compose logs -f futureai-api
```

**预期看到**（按 `Ctrl+C` 退出）：

```
using PostgreSQL as database
database migration started
FutureAI API started on port 3001
```

卡在 `database migration` 或直接退出，多半是 `.env` 里的 `POSTGRES_PASSWORD` 填错了。

## 6 服务器：验证三层是否打通

### 6.1 应用健康检查

```bash
curl -k --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/health
```

**预期**：`{"status":"ok"}`

### 6.2 首页有 HTML

```bash
curl -k -s --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/ | head -3
```

**预期**：`<!doctype html>` 开头的一段 HTML。

### 6.3 API 路径的 404 是 JSON

```bash
curl -k -s --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/api/nope
```

**预期**：`{"error":{"message":"Not found",...}}`

三条都对上，说明网关转发、应用响应、静态页面托管都正常。

### 6.4 确认 TRUSTED_PROXIES 生效

**这项最容易漏，后果也最严重**，它决定应用能不能认出用户的真实 IP。

从外部（你自己的电脑）打开 `https://futureaiapi.com`，故意输错几次密码，然后回服务器上看日志：

```bash
docker compose logs futureai-api | grep -i login
```

日志中来源 IP 的判定如下。

| 日志里的来源 IP | 判定 |
|---|---|
| 你自己的公网 IP | 正确 |
| `172.20.0.2` | 未生效 |

没生效的话，**所有用户会被登录限流当成同一个人**，任意几次失败就锁死所有人，而现象看起来像「密码错了」，很难联想到限流。

**修法**：核对 `deploy/futureai-api/.env` 里的 `GATEWAY_IP` 与 `deploy/gateway/docker-compose.yml` 里的 `ipv4_address`，两处都应该是 `172.20.0.2`。

## 7 服务器：首次登录并改密

### 7.1 登录并改密

浏览器打开 `https://futureaiapi.com`，用户名 `root`，密码是第 5.9 步设的 `INITIAL_ROOT_PASSWORD`。登录后立即修改密码。

### 7.2 清空 .env 里的初始密码

```bash
vi .env
```

把 `INITIAL_ROOT_PASSWORD=` 那行清空，保存退出。

## 8 服务器：换正式证书

> 域名还没配好解析就**跳过本章**，不影响使用，只是浏览器会显示证书警告，继续访问即可。等配好了再回来做。

### 8.1 加 A 记录

去你购买域名的服务商控制台（阿里云、Cloudflare、Namecheap 等），找到 DNS 解析设置，加一条记录。

| 类型 | 主机记录 | 值 |
|---|---|---|
| A | @ | 你的服务器公网 IP |

主机记录填 `@` 表示**域名本身**，所以这条让 `futureaiapi.com` 直接指向你的服务器。

> **注意**：这里填的域名要和 `deploy/gateway/.env` 里**完全一致**，否则证书验证会失败。

### 8.2 确认解析已生效

```bash
ping -c 1 futureaiapi.com
```

**预期看到**：返回的 IP 是你的服务器公网 IP。刚配好可能要等几分钟到几小时（DNS 缓存）。没生效就签不了证书。

### 8.3 确认 80 端口能从公网访问

防火墙和云安全组都要放行：Let's Encrypt 需要主动连回来验证。

### 8.4 签发证书

进入网关目录：

```bash
cd /opt/stacks/gateway
```

签发：

```bash
./scripts/certbot.sh issue
```

**它做了什么**：启动一个 certbot 容器向 Let's Encrypt 申请证书。Let's Encrypt 必须先确认「这个域名确实指向你这台机器」，办法是它去访问 `http://你的域名/.well-known/acme-challenge/xxx`，网关返回它要的内容。验证通过后证书下发，脚本自动复制到 `certs/` 并重载网关。

**预期看到**：一串 `Congratulations!` 之类的输出，最后是 `==> 证书已更新，重载网关`。

**如果报错**：最常见的是解析还没生效，或者 80 端口没放行。

### 8.5 验证证书

```bash
curl -I https://futureaiapi.com/health
```

正式证书浏览器天然信任，所以这里**不用** `-k`。**预期看到** `HTTP/2 200`。

浏览器打开应该是锁头图标、没有警告。

## 9 服务器：挂定时任务

> 本章与前两章无关，**即使没有域名也必须做**。三条 cron 里只有第一条是证书续期，另外两条是数据库备份与日志清理。

### 9.1 挂定时任务

```bash
sudo crontab -e
```

在文件末尾加这三行：

```
17 3 * * * cd /opt/stacks/gateway && ./scripts/certbot.sh renew >> /var/log/certbot-renew.log 2>&1
30 3 * * * /opt/stacks/futureai-api/backup.sh >> /var/log/futureai-api-backup.log 2>&1
0  4 * * * find /opt/stacks/gateway/logs -name '*.log' -mtime +14 -delete
```

**三条，一条都不能少。**

| 时间 | 做什么 | 漏了会怎样 |
|---|---|---|
| 每天 3:17 | 检查证书续期 | 证书 90 天过期，网站打不开。LE 已停发到期提醒邮件，没有任何预警 |
| 每天 3:30 | 备份数据库和 `.env` | 出事时没有可恢复的备份 |
| 每天 4:00 | 删掉 14 天前的网关日志 | **日志无限增长写满磁盘，数据库同盘会一起挂** |

第三条最容易漏。网关日志写在宿主机的 `/opt/stacks/gateway/logs/` 下，Docker 的日志上限**管不到它**（那只管容器的 stdout / stderr），只能靠主机侧清理。

`certbot.sh renew` 只在证书快到期时才真正续期，其余时候什么也不做。末尾那串 `>> /var/log/... 2>&1` 是把输出写进日志文件，方便以后回查。

> **注意**：必须用 `sudo`。日志写在 `/var/log/` 下，普通用户的 crontab 没有写权限，会静默失败，直到某天证书过期、网站打不开才发现。

> 五个字段的含义是 `分 时 日 月 星期`，`*` 表示「都行」。第一次跑 `crontab -e` 会问选哪个编辑器，选 `nano` 最简单，存盘是 `Ctrl+O` 回车、再 `Ctrl+X`。

### 9.2 确认定时任务已挂上

```bash
crontab -l
```

应该能看到刚才加的那三行。

### 9.3 手动跑一次续期

```bash
cd /opt/stacks/gateway
```

```bash
./scripts/certbot.sh renew
```

不必等到凌晨。手动跑一次才能确认它真的能用，而不是等三个月后才发现不行。

## 10 服务器：以后要再加一个服务

当前只部署了 futureai-api。这台机器以后还要放别的服务，做法是四处改动。

### 10.1 把那个服务接进共享网络

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

### 10.2 删掉它的 `ports:`

网关通过共享网络直连容器。发布到宿主机反而让它能被绕过网关访问。

### 10.3 网关加一个 server 块

复制 [gateway/templates/default.conf.template](gateway/templates/default.conf.template) 里 futureai-api 那个块，改四处：`server_name`、`access_log` 文件名、`set $xxx_up`（`服务名:端口`）、以及流式服务需要的超时设置。**模板文件末尾有逐条说明。**

### 10.4 给流式服务加超时（不是流式就跳过）

```nginx
proxy_read_timeout 600s;
proxy_buffering off;
```

> **注意**：那个服务如果是**流式**返回的（一次回答持续几十秒到几分钟），必须加这两行。漏了的话长回答会在中途被**静默截断**，连接是正常断开的，日志里看不出任何异常，现象只是「回答到一半卡住」。另外证书要重新签，SAN 里得有新域名。

### 10.5 在网关 .env 加域名，并同步改 filter

`deploy/gateway/.env` 加域名，同时改 `docker-compose.yml` 里的 `NGINX_ENVSUBST_FILTER`。

漏了后者的话，模板里新写的 `${NEW_DOMAIN}` 不会被替换，nginx 会当成字面量。

### 10.6 重新渲染并验证

```bash
cd /opt/stacks/gateway
```

```bash
docker compose up -d --force-recreate
```

```bash
docker compose exec gateway nginx -t
```
