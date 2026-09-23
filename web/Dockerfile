# 局域网文件助手 —— 前端镜像（部署在公网）
#
# 一个镜像通吃所有环境：VITE_API_BASE_URL 虽是构建期变量，但镜像内置了
# docker-entrypoint.d 生成 /config.js 的脚本，容器启动时用环境变量
# LANDRIVE_API_BASE_URL 覆盖，因此**同一个 tag 可部署到不同内网地址**，
# 无需为每个环境重新构建镜像。
#
# 构建（在仓库根目录执行）：
#   docker build -f web/Dockerfile -t lan-drive-web:1.0.0 web
#
# 运行：
#   docker run -d -p 80:80 \
#     -e LANDRIVE_API_BASE_URL='http://192.168.1.100:8080/api' \
#     lan-drive-web:1.0.0

# ---------- 构建阶段 ----------
FROM node:22-alpine AS build
WORKDIR /app

# 镜像内置的默认 API 地址（可被运行时环境变量覆盖）。留空则回退同源 /api。
ARG VITE_API_BASE_URL=""
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
ARG VITE_API_PROBE_TIMEOUT="6000"
ENV VITE_API_PROBE_TIMEOUT=${VITE_API_PROBE_TIMEOUT}

COPY package.json package-lock.json* ./
RUN npm install --include=dev --no-fund --no-audit

COPY . .
RUN npm run build

# ---------- 运行阶段 ----------
FROM nginx:alpine

# 站点配置（含 SPA 回退与 /config.js 防缓存）
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

# 运行时配置注入：nginx 官方镜像会自动执行 /docker-entrypoint.d/*.sh
COPY docker-entrypoint.d/40-lan-drive-config.sh /docker-entrypoint.d/40-lan-drive-config.sh
RUN chmod +x /docker-entrypoint.d/40-lan-drive-config.sh

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1/config.js >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
