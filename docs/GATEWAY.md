# 本地运行 Nginx 网关

在本地复现生产架构（域名分流 + TLS 终止），用于验证网关配置。不需要真实域名和证书，自签证书加 hosts 即可。

日常开发见 [DEV.md](DEV.md)，验证镜像见 [DOCKER.md](DOCKER.md)，部署到服务器见 [../deploy/README.md](../deploy/README.md)。

除第 4、5 章外，各章命令均在仓库根目录执行。

## 1 准备

**Docker Desktop 已启动**

```bash
docker info
```

能打印出信息即为已启动。

**仓库根目录存在 `.env`**

```bash
ls .env
```

`.env` 在刚克隆的仓库中不存在，缺失时第 3 章的 `docker compose up -d` 会在解析配置时即失败，报 `required variable POSTGRES_PASSWORD is missing a value`。配置方法见 [DEV.md](DEV.md) 的 1.2，或 [DOCKER.md](DOCKER.md) 第 1 章。

## 2 建共享网络

```bash
docker network create --subnet 172.20.0.0/16 --ip-range 172.20.128.0/17 gateway-proxy
```

`--ip-range` 不可省略：它保证网关的固定 IP `172.20.0.2` 不被其他容器占用，该地址一旦被占，网关将无法再启动。

> **注意**：报 `already exists` 说明此前建过且未清理，可跳过本章（网络已存在）；若要重建，先执行第 9 章。

## 3 起应用容器

```bash
docker compose up -d
```

镜像不存在时 compose 会自动构建，见 [DOCKER.md](DOCKER.md) 第 2 章。

> **注意**：若要验证发版用的镜像而非本机构建的镜像，先执行 `./deploy/build-image.sh linux/amd64`。该脚本会把镜像 `--load` 进本地，之后的 `up -d` 直接使用它。此操作会在仓库根目录留下约 14MB 的 `.tar.gz`，本机验证用不到，可以删除。

## 4 准备网关配置

本章与下一章的命令在 `deploy/gateway/` 下执行。先进入该目录：

```bash
cd deploy/gateway
```

复制配置模板（域名用默认值即可，本地不需要真实 DNS）：

```bash
cp .env.example .env
```

赋予脚本执行权限：

```bash
chmod +x scripts/*.sh
```

生成占位证书（脚本会打印自检结果）：

```bash
./scripts/self-signed.sh
```

## 5 起网关容器

启动网关：

```bash
docker compose up -d
```

校验配置：

```bash
docker compose exec gateway nginx -t
```

返回仓库根目录，后续各章均在此执行：

```bash
cd ../..
```

## 6 把应用接入共享网络

```bash
docker network connect gateway-proxy futureai-api
```

> **注意**：容器重建后这条连接会丢失，网关会开始返回 502，重跑上述命令即可。执行 `docker compose up -d` 换镜像之后尤其容易遗漏。

## 7 命令行验证

确认健康检查接口：

```bash
curl -k --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/health
```

预期返回 `{"status":"ok"}`。

确认首页：

```bash
curl -k -s --resolve futureaiapi.com:443:127.0.0.1 https://futureaiapi.com/
```

预期返回 HTML 页面，以 `<script src="/assets/index-xxx.js">` 开头。

> **注意**：必须使用 `--resolve`，不能使用 `-H "Host: ..."`。curl 连接 IP 地址时不发送 SNI，nginx 会落到拒绝握手的 `default_server` 块，得到的是 TLS 错误而非响应；`--resolve` 会同时设置 SNI 与 Host。

## 8 浏览器访问

命令行验证通过后，若要继续用浏览器查看页面，需先修改 hosts 文件。以下操作需要**管理员权限**，且会在系统中留下一条记录，第 9 章会要求删除它。

Windows：

```powershell
"127.0.0.1 futureaiapi.com" | Add-Content "$env:SystemRoot\System32\drivers\etc\hosts"
```

```powershell
ipconfig /flushdns
```

macOS / Linux：

```bash
echo "127.0.0.1 futureaiapi.com" | sudo tee -a /etc/hosts
```

随后访问 https://futureaiapi.com。证书警告是自签证书的正常表现，选择继续访问即可。

## 9 清理

断开应用与共享网络的连接：

```bash
docker network disconnect gateway-proxy futureai-api
```

停止网关：

```bash
(cd deploy/gateway && docker compose down)
```

删除共享网络：

```bash
docker network rm gateway-proxy
```

最后删除第 8 章加入 hosts 的那一行。

> 应用容器会保留。若要一并停止，在仓库根目录执行 `docker compose down`。

## 10 故障排查

下表按现象列出常见问题。

| 现象 | 处理 |
|---|---|
| 网关反复重启，日志提示证书不存在 | 证书未生成成功，重跑 `./scripts/self-signed.sh` |
| 网关无法启动，报 `Address already in use` | 建网络时漏了 `--ip-range`，见第 2 章 |
| 某个域名返回 502 | 对应服务未启动，或未接入共享网络，见第 6 章 |
| 修改模板未生效 | 改的是渲染产物。应修改 `templates/default.conf.template`，然后执行 `docker compose up -d --force-recreate` |
| 浏览器打不开，但 curl 正常 | 系统代理绕过了 hosts，把 `futureaiapi.com` 加入代理的直连规则 |

更深入的排查见 [deploy/NOTES.md](../deploy/NOTES.md#10-排查表)。
