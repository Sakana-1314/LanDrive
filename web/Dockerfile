# 局域网文件助手 —— 前端镜像
#
# 两种部署形态，同一个镜像都支持（由运行时环境变量切换，无需重新构建）：
#
#   A) 同源反代（推荐）：本容器把 /api 转发到后端容器。
#      docker run -d -p 80:80 -e LANDRIVE_API_PROXY=api:8080 lan-drive-web
#      → 浏览器只访问一个域名，不跨域、不需要 CORS，后端地址不外泄。
#
#   B) 跨域直连：前端直接请求内网 API（后端需配置 LANDRIVE_CORS_ALLOW）。
#      docker run -d -p 80:80 \
#        -e LANDRIVE_API_BASE_URL='https://api.example.com/api' lan-drive-web
#      → 不传 LANDRIVE_API_BASE_URL 时，回退到构建期 --build-arg HOST 注入的域名。
#
# 构建期也可通过 --build-arg HOST=<后端域名> 预置地址（仅 origin，不含 /api）。
#
# 构建（构建上下文为 web/ 目录）：
#   docker build -f web/Dockerfile -t lan-drive-web:1.0.0 web

# ---------- 构建阶段 ----------
FROM node:22-alpine AS build
WORKDIR /app

# 后端域名（仅 origin，不含 /api）。留空则前端走同源 /api。
# 例：--build-arg HOST=https://api.example.com
ARG HOST=""
ENV HOST=${HOST}
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

# /api 的默认处理（不反代 → JSON 404）。启用反代时由启动脚本覆盖此文件。
# 必须预先把目录与文件放进镜像：nginx.conf 会 include 它，
# 缺失会导致 nginx 启动直接失败。
COPY proxy.d/api.conf /etc/nginx/proxy.d/api.conf

COPY --from=build /app/dist /usr/share/nginx/html

# 运行时配置注入：nginx 官方镜像会自动执行 /docker-entrypoint.d/*.sh
COPY docker-entrypoint.d/40-lan-drive-config.sh /docker-entrypoint.d/40-lan-drive-config.sh
RUN chmod +x /docker-entrypoint.d/40-lan-drive-config.sh

# 反代目标（可选）。设置后本容器把 /api 转发到该地址，前端改用同源 /api：
#   docker run -e LANDRIVE_API_PROXY=api:8080 ...
# 留空则保持跨域直连形态（用 LANDRIVE_API_BASE_URL 或构建期 HOST）。
ENV LANDRIVE_API_PROXY=""

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1/config.js >/dev/null 2>&1 || exit 1

CMD ["nginx", "-g", "daemon off;"]
