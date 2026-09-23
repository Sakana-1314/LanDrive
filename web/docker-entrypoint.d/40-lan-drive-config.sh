#!/bin/sh
# 在 nginx 启动前生成 /config.js，把内网 API 地址注入为**运行时**配置。
#
# 为什么需要它：VITE_API_BASE_URL 是 Vite 的构建期变量，一旦打进产物就无法更改。
# 但同一份前端镜像可能要部署到测试/生产等不同内网地址，为每个环境重新构建
# 镜像既慢又容易出错。这里改为运行时读取环境变量并生成 config.js，
# 前端启动时优先读取它（见 web/src/api/index.ts）。
set -eu

TARGET=/usr/share/nginx/html/config.js

# 允许用环境变量覆盖；未设置时沿用构建期注入的值（由 41 号脚本或构建产物兜底）。
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
  echo "[lan-drive] 运行时 API 地址：${API_BASE_URL}"
else
  echo "[lan-drive] 未设置 LANDRIVE_API_BASE_URL，前端将使用构建期默认值（同源 /api）"
fi
