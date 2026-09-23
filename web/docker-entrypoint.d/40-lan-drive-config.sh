#!/bin/sh
# 容器启动前生成两份运行时配置（nginx 官方镜像会自动执行 /docker-entrypoint.d/*.sh）：
#
#   1. /config.js                  —— 告诉前端"接口地址是什么"
#   2. /etc/nginx/proxy.d/api.conf —— 决定本容器是否把 /api 反代到后端容器
#
# 为什么需要运行时注入：后端域名/容器地址随环境而变，若写死在构建期，
# 同一个镜像就无法在测试与生产之间复用，改地址都要重新构建。
#
# 两种形态（见 nginx.conf 文件头）：
#   A) LANDRIVE_API_PROXY=api:8080  → nginx 反代 /api，前端走同源 /api（不跨域、免 CORS）
#   B) 都不设置                      → 反代关闭，前端用构建期 HOST 注入的地址（跨域）
set -eu

HTML_DIR=/usr/share/nginx/html
CONFIG_JS="$HTML_DIR/config.js"
PROXY_CONF=/etc/nginx/proxy.d/api.conf

# 显式指定的接口地址（含 /api），优先级最高
API_BASE_URL="${LANDRIVE_API_BASE_URL:-}"
# 同源反代的后端地址，形如 api:8080 或 http://api:8080
API_PROXY="${LANDRIVE_API_PROXY:-}"
PROBE_TIMEOUT="${LANDRIVE_API_PROBE_TIMEOUT:-6000}"
PROXY_CONNECT_TIMEOUT="${LANDRIVE_API_PROXY_CONNECT_TIMEOUT:-10}"
PROXY_READ_TIMEOUT="${LANDRIVE_API_PROXY_READ_TIMEOUT:-600}"
PROXY_SEND_TIMEOUT="${LANDRIVE_API_PROXY_SEND_TIMEOUT:-600}"
# 用于在请求时解析上游主机名（见下方 proxy_pass 说明）。
# Docker 默认使用内嵌 DNS 127.0.0.11；其它环境回退到系统 resolv.conf 里的第一个地址。
DNS_RESOLVER="${LANDRIVE_DNS_RESOLVER:-}"

# 去掉结尾斜杠，避免拼出 //api 这种地址。
# 注意 sed 分隔符用 | 而不是 :，否则与要匹配的 :// 冲突。
API_BASE_URL=$(printf '%s' "$API_BASE_URL" | sed 's|/*$||')

# 所有"数字秒/毫秒"类变量统一校验：非法值回退默认。
# 这些值会直接拼进 nginx 配置，非法内容（如 "abc"）会导致 nginx 无法启动。
positive_int() {
  # $1=值 $2=默认值
  case "${1:-}" in
    ''|*[!0-9]*) printf '%s' "$2" ;;
    *) printf '%s' "$1" ;;
  esac
}
PROBE_TIMEOUT=$(positive_int "$PROBE_TIMEOUT" 6000)
PROXY_CONNECT_TIMEOUT=$(positive_int "$PROXY_CONNECT_TIMEOUT" 10)
PROXY_READ_TIMEOUT=$(positive_int "$PROXY_READ_TIMEOUT" 600)
PROXY_SEND_TIMEOUT=$(positive_int "$PROXY_SEND_TIMEOUT" 600)

# ---------- 解析反代目标 ----------
if [ -n "$API_PROXY" ]; then
  # 允许写成 http://api:8080 / api:8080/ 等形式，统一成 host:port
  PROXY_HOST=$(printf '%s' "$API_PROXY" | sed -e 's|^https\?://||' -e 's|/*$||')

  # 校验必须在**整个字符串**上进行：grep 是按行匹配的，
  # 含换行的输入（如 $'api:8080\n} location /evil {...'）会让逐行匹配通过，
  # 却把额外指令注入 nginx 配置。case 的 glob 匹配的是完整值，不受换行影响。
  case "$PROXY_HOST" in
    ''|*[!A-Za-z0-9._:-]*)
      echo "[lan-drive] 错误：LANDRIVE_API_PROXY 取值非法（'$API_PROXY'）" >&2
      echo "[lan-drive]       只允许 http(s)://主机[:端口]，例如 api:8080。" >&2
      exit 1
      ;;
    [-.]*)
      # 以 - 或 . 开头不是合法主机名，且会生成 nginx 无法解析的配置。
      echo "[lan-drive] 错误：LANDRIVE_API_PROXY 主机名不能以 '-' 或 '.' 开头（'$API_PROXY'）" >&2
      exit 1
      ;;
  esac

  PROXY_NAME=${PROXY_HOST%%:*}
  case "$PROXY_HOST" in
    *:*) PROXY_PORT=${PROXY_HOST#*:} ;;
    *) PROXY_PORT="" ;;
  esac
  # 主机名为空（如 ":8080"）会生成 http://:8080，nginx 无法解析。
  if [ -z "$PROXY_NAME" ]; then
    echo "[lan-drive] 错误：LANDRIVE_API_PROXY 缺少主机名（'$API_PROXY'）" >&2
    echo "[lan-drive]       应为 主机:端口，例如 api:8080。" >&2
    exit 1
  fi
  # 值里出现了 ':' 就必须带有效端口：'api:' 这类漏写端口属笔误，
  # 静默补成 8080 会让人以为配的是别的端口，这里直接报错。
  case "$PROXY_HOST" in
    *:)
      echo "[lan-drive] 错误：LANDRIVE_API_PROXY 的端口为空（'$API_PROXY'）" >&2
      echo "[lan-drive]       请写成 主机:端口（如 api:8080），或只写主机名用默认 8080。" >&2
      exit 1
      ;;
  esac

  if [ -n "$PROXY_PORT" ]; then
    case "$PROXY_PORT" in
      *[!0-9]*)
        echo "[lan-drive] 错误：LANDRIVE_API_PROXY 端口必须是数字（'$API_PROXY'）" >&2
        exit 1
        ;;
    esac
    if [ "$PROXY_PORT" -lt 1 ] || [ "$PROXY_PORT" -gt 65535 ]; then
      echo "[lan-drive] 错误：LANDRIVE_API_PROXY 端口超出 1-65535（'$API_PROXY'）" >&2
      exit 1
    fi
    PROXY_HOST="$PROXY_NAME:$PROXY_PORT"
  else
    # 未指定端口时补上后端默认端口 8080
    PROXY_HOST="$PROXY_NAME:8080"
  fi

  # 解析器：未显式指定时，Docker 环境（存在 /etc/resolv.conf 且含 127.0.0.11）
  # 用内嵌 DNS；否则取系统 resolv.conf 的第一个 nameserver，最后回退公共 DNS。
  if [ -z "$DNS_RESOLVER" ]; then
    if grep -q '127\.0\.0\.11' /etc/resolv.conf 2>/dev/null; then
      DNS_RESOLVER=127.0.0.11
    else
      DNS_RESOLVER=$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf 2>/dev/null || true)
      [ -n "$DNS_RESOLVER" ] || DNS_RESOLVER=127.0.0.53
    fi
  fi

  # proxy_pass 用**变量**而不是直接写主机名，并配 resolver：
  # 直接写主机名时 nginx 会在**启动时**解析一次，后端容器尚未就绪/域名暂不可解析
  # 会导致整个前端容器起不来（[emerg] host not found in upstream）。
  # 用变量可把解析推迟到请求时，后端晚起也只是 /api 返回 502，页面照常可用。
  #
  # 注意 proxy_pass 用变量后又**不能**带 URI 部分，因此不能靠结尾斜杠剥前缀 ——
  # 好在我们的需求正是保留 /api 前缀（后端路由就是 /api/**）。
  cat > "$PROXY_CONF" <<EOF
# 本文件由 40-lan-drive-config.sh 生成，请勿手工修改（重启容器会覆盖）。
# 请求时解析上游，避免后端未就绪导致 nginx 无法启动。
resolver ${DNS_RESOLVER} valid=10s ipv6=off;

location /api/ {
    set \$lanfs_backend "http://${PROXY_HOST}";
    proxy_pass \$lanfs_backend;
    proxy_http_version 1.1;

    # 透传真实客户端信息；后端 LANDRIVE_TRUST_PROXY=true 时会据此记录来源 IP。
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_set_header Authorization \$http_authorization;
    proxy_set_header Connection "";

    # 大文件上传/下载可能很慢，读写超时放宽；连接超时保持短，便于快速暴露后端未就绪。
    proxy_connect_timeout ${PROXY_CONNECT_TIMEOUT}s;
    proxy_read_timeout ${PROXY_READ_TIMEOUT}s;
    proxy_send_timeout ${PROXY_SEND_TIMEOUT}s;

    # 上传/下载直接透传，不在 nginx 侧缓冲整个请求体。
    proxy_request_buffering off;
    proxy_buffering off;
}
EOF

  # 反代形态下前端必须走同源 /api，否则浏览器仍会跨域直连内网，反代就白配了。
  # 用户显式设置了 LANDRIVE_API_BASE_URL 时以其为准（允许反代 + 绝对地址并存）。
  if [ -z "$API_BASE_URL" ]; then
    API_BASE_URL="/api"
  fi

  echo "[lan-drive] 已启用 /api 同源反代 → http://${PROXY_HOST}（前端走 ${API_BASE_URL}）"
else
  # 保持镜像内的默认文件（返回 JSON 404），确保 /api 不会落到 SPA 回退返回 HTML。
  if [ ! -f "$PROXY_CONF" ]; then
    cat > "$PROXY_CONF" <<'EOF'
location /api/ {
    default_type application/json;
    add_header Cache-Control "no-store";
    return 404 '{"error":"接口未通过本容器反代"}';
}
EOF
  fi
  echo "[lan-drive] 未启用 /api 反代（可设 LANDRIVE_API_PROXY=api:8080 开启）"
fi

# ---------- 生成 /config.js ----------
# API_BASE_URL 为空时前端回退到构建期注入的 __API_HOST__，再回退到同源 /api。
cat > "$CONFIG_JS" <<EOF
// 由 web/docker-entrypoint.d/40-lan-drive-config.sh 在容器启动时生成。
// 修改后重启容器即可生效，无需重新构建镜像。
window.__LANDRIVE_CONFIG__ = {
  apiBaseUrl: "${API_BASE_URL}",
  probeTimeout: ${PROBE_TIMEOUT}
};
EOF

if [ -n "$API_BASE_URL" ]; then
  echo "[lan-drive] 接口地址（运行时注入）：${API_BASE_URL}"
else
  echo "[lan-drive] 未指定接口地址，使用镜像构建期注入的后端域名"
fi
