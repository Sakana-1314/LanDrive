# 局域网文件助手 —— 内网 API 服务镜像
#
# 本镜像**只包含后端 API**（server/），不包含前端页面。
# 前端（web/）独立构建后部署到公网静态托管，通过跨域访问本服务。
#
# 构建（构建上下文为 server/ 目录）：
#   docker build -f server/Dockerfile -t lan-drive-api:1.0.0 server
#   docker build -f server/Dockerfile -t lan-drive-api:1.0.0 .   # 也可用仓库根目录，见下方说明
#
# 运行示例（需外部 MySQL）：
#   docker run -d --name lanfs-api -p 8080:8080 \
#     -e LANDRIVE_MYSQL_DSN='lanfs:pass@tcp(10.0.0.5:3306)/lanfs' \
#     -e LANDRIVE_JWT_SECRET="$(openssl rand -hex 32)" \
#     -e LANDRIVE_ADMIN_PASSWORD='change-me' \
#     -e LANDRIVE_CORS_ALLOW='https://files.your-company.com' \
#     -v file-data:/data lan-drive-api:1.0.0

# ---------- 构建阶段 ----------
FROM golang:1.24-bookworm AS build
WORKDIR /src

# 构建上下文为 server/（CI 与 Makefile 均如此）：
#   docker build -f server/Dockerfile -t ... server
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG VERSION=1.0.0
RUN CGO_ENABLED=0 go build \
      -tags nomsgpack -buildvcs=false -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/landrive ./cmd/landrive

# ---------- 运行阶段 ----------
FROM debian:bookworm-slim

# ca-certificates 供将来可能的 HTTPS 出网；tzdata 让日志时间可读（数据仍按 UTC 存储）。
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata wget \
 && rm -rf /var/lib/apt/lists/*

# 非 root 运行；数据目录归属该用户。
RUN useradd --system --create-home --uid 10001 landrive \
 && mkdir -p /data \
 && chown -R landrive:landrive /data

COPY --from=build /out/landrive /usr/local/bin/landrive

USER landrive
WORKDIR /home/landrive

ENV LANDRIVE_LISTEN=:8080 \
    LANDRIVE_DATA_DIR=/data \
    TZ=Asia/Shanghai

EXPOSE 8080
VOLUME ["/data"]

# 健康检查直接用内置接口（同时校验数据库连通性）。
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/landrive"]
