# 在本地跑 Nginx 网关

验证生产架构（域名分流 + TLS）用。日常开发见 [DEVELOPMENT.md](DEVELOPMENT.md)，
只想验证镜像见 [DOCKER.md](DOCKER.md)。

---

## 1. 建共享网络

```bash
docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

`--ip-range` 不能省。它让网关的固定 IP `172.20.0.2` 不会被别的容器抢走 ——
网关停机时其他容器如果占了这个地址，网关就再也起不来了。

## 2. 起应用

```bash
./deploy/build-image.sh linux/amd64
docker compose up -d
```

## 3. 起网关

```bash
cd deploy/gateway
cp .env.example .env          # 域名用默认的即可，本地不需要真实 DNS
chmod +x scripts/*.sh
./scripts/self-signed.sh      # 生成占位证书，脚本会打印自检结果
docker compose up -d
docker compose exec gateway nginx -t
```

## 4. 把应用接进共享网络

```bash
docker network connect gateway-proxy token-hub
```

网关在 `gateway-proxy` 上，应用在 `token-hub_default` 上，两者不通。这条命令给应用
再插一块"网卡"，它原来的网络不受影响。

> ⚠️ **容器重建后这条接线会丢**，网关会突然开始 502。重跑上面这条命令即可。
> `docker compose up -d` 换镜像之后尤其容易忘。

## 5. 访问

```bash
curl -k --resolve token.example.com:443:127.0.0.1 https://token.example.com/health
# {"status":"ok"}

curl -k -s --resolve token.example.com:443:127.0.0.1 https://token.example.com/ | head -3
# HTML 页面（<script src="/assets/index-xxx.js"> 开头）
```

> **必须用 `--resolve`，不能用 `-H "Host: ..."`。** curl 连接 IP 地址时不发 SNI，
> nginx 会落到 `default_server` 那个拒绝握手的块，你会看到 TLS 错误而不是响应。
> `--resolve` 同时把 SNI 和 Host 设成目标域名。

浏览器访问要先改 hosts（**管理员权限**）：

```powershell
"127.0.0.1 token.example.com" | Add-Content "$env:SystemRoot\System32\drivers\etc\hosts"
ipconfig /flushdns
```

然后打开 **https://token.example.com**。证书警告是自签证书的正常表现，点「继续前往」。

---

## 清理

```bash
docker network disconnect gateway-proxy token-hub
cd deploy/gateway && docker compose down
docker network rm gateway-proxy
```

hosts 里那三行也删掉。

---

## 出问题

| 现象 | 原因 |
|---|---|
| 网关反复重启，日志说证书不存在 | 证书没生成成功，重跑 `./scripts/self-signed.sh` |
| 网关起不来，`Address already in use` | 网络建的时候漏了 `--ip-range`，见第 1 步 |
| 某个域名返回 502 | 那个服务没起，或没接进共享网络（第 4 步） |
| 改了模板没生效 | 改的是渲染产物。要改 `templates/default.conf.template`，然后 `docker compose up -d --force-recreate` |
| 浏览器打不开，但 curl 正常 | 系统代理绕过了 hosts，把 `*.example.com` 加进代理的直连规则 |

更深入的排查见 [deploy/NOTES.md](../deploy/NOTES.md#排查表)。
