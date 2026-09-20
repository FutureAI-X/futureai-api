# FutureAI API — 常用开发与构建命令
#
# ⚠️ 关于构建顺序（重要，且反直觉）：
#
# 根包 main 通过 go:embed 内嵌 web/dist。go:embed 要求该目录存在且非空，
# 否则**编译失败**——是编译期错误，不是运行时提示。因此在干净的 clone 上，
# `go build` 和 `go test ./...` 都必须先构建前端。
#
# 本 Makefile 已把依赖写在目标里：build / run 都会先跑 web。
#
# 受影响的另一处是 `go test ./...`：它会把根包也纳入编译，于是整个命令失败。
# 所以下面的 test / vet 目标显式把根包排除掉。这样做不损失覆盖率——
# 根包里没有任何测试文件，所有测试都在子包（common / webui / middleware /
# controller / model）。这个处理方式与 new-api 一致。

ROOT_MODULE := $(shell go list -m)
WEB_DIR     := web

IMAGE    ?= futureai-api
TAG      ?= latest
PLATFORM ?= linux/amd64

# 排除根包后的包列表
PKGS = $(shell go list -e ./... | grep -vxF "$(ROOT_MODULE)")

.PHONY: all help web run build test vet fmt docker save clean

all: web run

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## web: 构建前端产物到 web/dist
web:
	cd $(WEB_DIR) && npm ci && npm run build

## run: 构建前端后启动后端（开发用）
run: web
	go run main.go

## build: 构建前端后编译后端二进制
build: web
	go build -trimpath -ldflags="-s -w" -o futureai-api .

## test: 运行全部测试（根包除外，理由见文件顶部说明）
test:
	go test $(PKGS)

## vet: 静态检查（根包除外，同上）
vet:
	go vet $(PKGS)

## fmt: 格式化
fmt:
	gofmt -w $(shell git ls-files '*.go' | grep -v '^web/')

## docker: 构建镜像。PLATFORM 可选 linux/amd64 或 linux/arm64
docker:
	docker buildx build --platform $(PLATFORM) -t $(IMAGE):$(TAG) --load .

## save: 构建并导出镜像压缩包，用于传到服务器
save: docker
	docker save $(IMAGE):$(TAG) | gzip > $(IMAGE)-$(TAG)-$(subst linux/,,$(PLATFORM)).tar.gz
	@echo "已导出 $(IMAGE)-$(TAG)-$(subst linux/,,$(PLATFORM)).tar.gz"

## clean: 清理构建产物
clean:
	rm -rf $(WEB_DIR)/dist futureai-api
