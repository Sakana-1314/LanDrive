#!/bin/sh
# 在 nginx 启动前生成 /config.js，把内网 API 地址注入为**运行时**配置。
#
# 为什么需要它：后端域名默认在构建期由 HOST build-arg 注入（打进产物即可用），
# 但同一份镜像若要部署到测试/生产等不同内网地址，为每个环境重新构建镜像既慢
# 又容易出错。这里提供运行时覆盖：容器启动时生成 config.js，
# 前端优先读取它（见 web/src/api/index.ts 的 resolveApiBaseUrl）。
#
# 不设置 LANDRIVE_API_BASE_URL 时写入空串，前端会回退到构建期注入的 __API_HOST__。
set -eu

TARGET=/usr/share/nginx/html/config.js

# 允许用环境变量覆盖；未设置时留空，让前端用构建期注入的后端域名。
API_BASE_URL="${LANDRIVE_API_BASE_URL:-}"
PROBE_TIMEOUT="${LANDRIVE_API_PROBE_TIMEOUT:-6000}"

# 去掉结尾斜杠，避免拼出 //api 这种地址。
API_BASE_URL=$(printf '%s' "$API_BASE_URL" | sed 's:/*$::')

cat > "$TARGET" <<EOF
// 由 web/docker-entrypoint.d/40-lan-drive-config.sh 在容器启动时生成。
// 修改后重启容器即可生效，无需重新构建镜像。
window.__LANDRIVE_CONFIG__ = {
  apiBaseUrl: "${API_BASE_URL}",
  probeTimeout: ${PROBE_TIMEOUT}
};
EOF

if [ -n "$API_BASE_URL" ]; then
  echo "[lan-drive] 运行时覆盖 API 地址：${API_BASE_URL}"
else
  echo "[lan-drive] 未设置 LANDRIVE_API_BASE_URL，使用镜像构建期注入的后端域名"
fi
