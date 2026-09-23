# 局域网文件助手 —— 构建入口
#
# 目录结构（前后端分离）：
#   server/  Go + Gin 内网 API，编译为单一二进制，部署在内网
#   web/     Vue 3 前端，构建为静态站点，部署在公网
#
# 产物：
#   bin/landrive        内网 API 二进制
#   web/dist/           前端静态站点（部署到公网静态托管/CDN）
SHELL := /bin/bash

BIN      := bin
VERSION  ?= 1.0.0
SERVER   := server
WEB      := web
GO       ?= go

# 与同工作区其它项目一致的构建参数：
#   -tags nomsgpack  去掉 gin 内置但用不到的 msgpack 绑定，显著减小体积
#   -trimpath -s -w  裁剪路径与符号表
GOFLAGS := -tags nomsgpack -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$(VERSION)"
CGO_ENABLED := 0

.PHONY: help all server webinstall web webbuild build run webdev test vet fmt check \
        docsinstall docs docsdev \
        docker pull up up-all up-build down down-all clean clean-data \
        build-linux-amd64 build-linux-arm64

help:
	@echo "局域网文件助手 — 常用命令（前后端分离）"
	@echo ""
	@echo "  前端（部署到公网）"
	@echo "    make webinstall   安装前端依赖"
	@echo "    make web          构建前端静态站点 → $(WEB)/dist"
	@echo "    make webdev       前端开发服务器（:5173，已代理 /api 到本机后端）"
	@echo ""
	@echo "  后端（部署到内网）"
	@echo "    make server       编译 API 二进制 → $(BIN)/landrive"
	@echo "    make run          本地运行 API（需 MySQL，见 README）"
	@echo ""
	@echo "  校验"
	@echo "    make test         go test + 前端类型检查"
	@echo "    make check        gofmt/vet/test + 前端类型检查"
	@echo ""
	@echo "  部署"
	@echo "    make up           启动内网 API + MySQL（拉取 ghcr 固定 tag 镜像）"
	@echo "    make up-all       一体化启动（MySQL + API + 前端，单机内网）"
	@echo "    make down         停止容器"
	@echo "    make docker       本地构建镜像（server + web）"
	@echo "    make pull         拉取 ghcr 上的固定 tag 镜像"
	@echo ""
	@echo "  文档站（VitePress，源码在 docs/websites）"
	@echo "    make docsinstall  安装文档站依赖"
	@echo "    make docs         构建文档站 → docs/websites/.vitepress/dist"
	@echo "    make docsdev      文档站开发服务器（:5173）"
	@echo ""
	@echo "  make build          前后端全部构建（server + web）"

all: build

## 前后端全部构建
build: server web

# ============ 后端（内网 API） ============

## 编译 API 二进制
server:
	mkdir -p $(BIN)
	cd $(SERVER) && CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o ../$(BIN)/landrive ./cmd/landrive
	@echo "✅ 内网 API 已构建：$(BIN)/landrive（$(VERSION)）"

## 本地运行 API（需要可用的 MySQL，环境变量见 README）
run:
	cd $(SERVER) && $(GO) run ./cmd/landrive

## 交叉编译
define build-target
	cd $(SERVER) && mkdir -p ../$(BIN)
	cd $(SERVER) && CGO_ENABLED=$(CGO_ENABLED) GOOS=$(1) GOARCH=$(2) $(GO) build $(GOFLAGS) -o ../$(BIN)/landrive-$(1)-$(2) ./cmd/landrive
	@echo "✅ $(BIN)/landrive-$(1)-$(2)"
endef

build-linux-amd64:
	$(call build-target,linux,amd64)

build-linux-arm64:
	$(call build-target,linux,arm64)

# ============ 前端（公网静态站点） ============

## 安装前端依赖（--include=dev：防止 NODE_ENV=production 跳过 devDependencies）
webinstall:
	cd $(WEB) && npm install --include=dev --no-fund --no-audit

## 构建前端静态站点。部署前请传 HOST=<后端域名>（或写入 web/.env.production）指向内网 API。
web: webinstall
	cd $(WEB) && npm run build
	@echo "✅ 前端已构建：$(WEB)/dist（部署时把该目录内容放到公网静态托管）"
	@echo "   提醒：后端 LANDRIVE_CORS_ALLOW 必须包含前端访问域名，否则跨域会被拦截。"
	@echo "   提示：用 HOST=https://<后端域名> make web 可在构建期注入接口地址。"

## 前端开发服务器（vite，代理 /api 到本机 :8080）
webdev:
	cd $(WEB) && npm run dev

# ============ 文档站（VitePress） ============

## 安装文档站依赖（--include=dev：NODE_ENV=production 时会跳过 devDependencies，
## 而 VitePress 本身就在 devDependencies 里）
docsinstall:
	cd docs/websites && npm install --include=dev --no-fund --no-audit

## 构建文档站
docs: docsinstall
	cd docs/websites && npm run build
	@echo "✅ 文档站已构建：docs/websites/.vitepress/dist"

## 文档站开发服务器
docsdev:
	cd docs/websites && npm run dev

# ============ 校验 ============

## 测试前先确保前端依赖已安装：否则 npx 会去远端拉取 vue-tsc，
## 与项目锁定的 typescript 版本不兼容而报 ERR_PACKAGE_PATH_NOT_EXPORTED。
test: webinstall
	cd $(SERVER) && $(GO) test ./... -count=1
	cd $(WEB) && npm run typecheck
	cd $(WEB) && npm test
	@echo "✅ 测试与类型检查通过（含前端网络判定测试）"

vet:
	cd $(SERVER) && $(GO) vet ./...

fmt:
	cd $(SERVER) && gofmt -w ./cmd ./internal

check: webinstall
	@echo "== gofmt =="; test -z "$$(cd $(SERVER) && gofmt -l ./cmd ./internal)" || { cd $(SERVER) && gofmt -l ./cmd ./internal; exit 1; }
	@echo "== go vet =="; cd $(SERVER) && $(GO) vet ./...
	@echo "== go test =="; cd $(SERVER) && $(GO) test ./... -count=1
	@echo "== 前端类型检查 =="; cd $(WEB) && npm run typecheck
	@echo "== 前端网络判定测试 =="; cd $(WEB) && npm test
	@echo "✅ 全部检查通过"

# ============ 部署 ============

## 本地构建两个镜像（与 CI 推送的 tag 保持一致）
docker:
	docker build -f $(SERVER)/Dockerfile -t lan-drive-api:$(VERSION) $(SERVER)
	docker build -f $(WEB)/Dockerfile -t lan-drive-web:$(VERSION) $(WEB)
	@echo "✅ 镜像 lan-drive-api:$(VERSION) / lan-drive-web:$(VERSION)"

## 拉取 ghcr 上的固定 tag 镜像
pull:
	docker pull ghcr.io/sakana-1314/lan-drive:server
	docker pull ghcr.io/sakana-1314/lan-drive:web
	@echo "✅ 已拉取 server / web 镜像"

## 启动内网 API + MySQL（使用 ghcr 镜像）
up:
	docker compose pull app
	docker compose up -d
	@echo "✅ 已启动。请确认 LANDRIVE_CORS_ALLOW 已包含前端域名。"

## 一体化启动（MySQL + API + 前端，适用于前后端同机部署）
up-all:
	docker compose -f docker-compose.full.yml pull
	docker compose -f docker-compose.full.yml up -d
	@echo "✅ 已启动，浏览器访问 http://<服务器内网IP>/"

## 从源码构建并启动（不使用 ghcr 镜像）
up-build:
	docker compose up -d --build
	@echo "✅ 已从源码构建并启动。"

down:
	docker compose down

down-all:
	docker compose -f docker-compose.full.yml down

clean:
	rm -rf $(BIN) $(WEB)/dist $(WEB)/node_modules
	@echo "已清理（data/ 目录保留）"

## 清理本地运行数据（谨慎：会删除全部文件）
clean-data:
	rm -rf data
	@echo "已清理本地 data/ 目录（Docker 数据卷请用 docker compose down -v）"
