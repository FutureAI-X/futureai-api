# 在本地跑 Nginx 网关

在本地复现生产架构（域名分流 + TLS 终止），用来验证网关配置 ——
**不需要真实域名和证书**，自签证书 + hosts 就够。

日常开发看 [DEV.md](DEV.md)；只想验证镜像看 [DOCKER.md](DOCKER.md)；
部署到服务器看 [../deploy/README.md](../deploy/README.md)。

## 本文路线

| 节 | 做什么 |
|---|---|
| 一 | 准备：Docker Desktop + `.env` |
| 二 | 建共享网络 `gateway-proxy` |
| 三 | 起应用容器 |
| 四 | 起网关容器 |
| 五 | 把应用接进共享网络 |
| 六 | 访问验证 |

第七节是清理，第八节是排查。

> 除第四节外，各节命令都在**仓库根目录**执行。

---

## 一 准备

| 要什么 | 怎么确认 |
|---|---|
| Docker Desktop 已启动 | `docker info` 能打印出信息 |
| 仓库根目录有 `.env` | `ls .env` |

`.env` 干净 clone 上不存在，缺了它第三节的 `docker compose up -d` 会在解析配置时
就失败：`required variable POSTGRES_PASSWORD is missing a value`。
配置方法见 [DEV.md](DEV.md) 第二节，或 [DOCKER.md](DOCKER.md) 第一节。

---

## 二 建共享网络

```bash
docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

`--ip-range` 不能省。它让网关的固定 IP `172.20.0.2` 不会被别的容器抢走 ——
网关停机时其他容器如果占了这个地址，网关就再也起不来了。

> 报 `already exists` 说明之前建过没清干净，直接跳到下一节即可（网络已在）。
> 要重建先跑第七节。

---

## 三 起应用

```bash
docker compose up -d
```

镜像不存在时 compose 会自动构建（见 [DOCKER.md](DOCKER.md) 第二节）。

> 想验证**真正要发版的那个镜像**而不是本机构建的，先跑
> `./deploy/build-image.sh linux/amd64`。它会把镜像 `--load` 进本地，
> 之后的 `up -d` 就直接拿它用。
> 注意这会在仓库根目录留下一个约 14MB 的 `.tar.gz`，本机验证用不上，可以删。

---

## 四 起网关

本节命令都在 `deploy/gateway/` 里执行：

```bash
cd deploy/gateway

cp .env.example .env          # 域名用默认的即可，本地不需要真实 DNS
chmod +x scripts/*.sh
./scripts/self-signed.sh      # 生成占位证书，脚本会打印自检结果
docker compose up -d
docker compose exec gateway nginx -t

cd ../..                      # 回仓库根，后面各节都在那里执行
```

---

## 五 把应用接进共享网络

```bash
docker network connect gateway-proxy futureai-api
```

网关在 `gateway-proxy` 上，应用在 `futureai-api_default` 上，两者不通。这条命令给应用
再插一块「网卡」，它原来的网络不受影响。

> ⚠️ **容器重建后这条接线会丢**，网关会突然开始 502。重跑上面这条命令即可。
> `docker compose up -d` 换镜像之后尤其容易忘。

---

## 六 访问

```bash
curl -k --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/health
# {"status":"ok"}

curl -k -s --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/ | head -3
# HTML 页面（<script src="/assets/index-xxx.js"> 开头）
```

> **必须用 `--resolve`，不能用 `-H "Host: ..."`。** curl 连接 IP 地址时不发 SNI，
> nginx 会落到 `default_server` 那个拒绝握手的块，你会看到 TLS 错误而不是响应。
> `--resolve` 同时把 SNI 和 Host 设成目标域名。

浏览器访问要先改 hosts（**管理员权限**），二选一：

```powershell
# Windows
"127.0.0.1 futureaiapi.com" | Add-Content "$env:SystemRoot\System32\drivers\etc\hosts"
ipconfig /flushdns
```

```bash
# macOS / Linux
echo "127.0.0.1 futureaiapi.com" | sudo tee -a /etc/hosts
```

然后打开 **https://futureaiapi.com**。证书警告是自签证书的正常表现，点「继续前往」。

---

## 七 清理

```bash
docker network disconnect gateway-proxy futureai-api
(cd deploy/gateway && docker compose down)
docker network rm gateway-proxy
```

再把第六节加进 hosts 的**那一行**删掉。

> 应用容器会留着（下一节还要用的话正合适）。要一起停掉，在仓库根跑
> `docker compose down`。

---

## 八 出问题

| 现象 | 原因 |
|---|---|
| 网关反复重启，日志说证书不存在 | 证书没生成成功，重跑 `./scripts/self-signed.sh` |
| 网关起不来，`Address already in use` | 网络建的时候漏了 `--ip-range`，见第二节 |
| 某个域名返回 502 | 那个服务没起，或没接进共享网络（第五节） |
| 改了模板没生效 | 改的是渲染产物。要改 `templates/default.conf.template`，然后 `docker compose up -d --force-recreate` |
| 浏览器打不开，但 curl 正常 | 系统代理绕过了 hosts，把 `*.example.com` 加进代理的直连规则 |

更深入的排查见 [deploy/NOTES.md](../deploy/NOTES.md#排查表)。
