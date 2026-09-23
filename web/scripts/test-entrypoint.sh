#!/bin/sh
# 前端镜像运行时配置的回归测试（entrypoint 脚本 + 生成的 nginx 配置）。
#
# 为什么需要它：这两处都属于"配置正确才工作"的代码，写错了本地不会报错，
# 要等容器启动或用户访问才暴露。已经在这些点上踩过坑：
#   - sed 分隔符用了 : 与待匹配的 :// 冲突，脚本直接报错；
#   - grep 按行匹配，含换行的 LANDRIVE_API_PROXY 绕过校验并注入 nginx 指令；
#   - proxy_pass 直接写主机名，后端未就绪时 nginx 整个起不来；
#   - /api 未反代时落到 SPA 回退返回 200 HTML，前端误判为"内网已连通"。
#
# 用法：sh web/scripts/test-entrypoint.sh
# 依赖：sh + grep/sed/awk。有 nginx 时会额外校验生成的配置能被 nginx 解析。
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ENTRYPOINT="$ROOT/docker-entrypoint.d/40-lan-drive-config.sh"

# 脚本写的是镜像内的绝对路径，测试时重定向到临时目录。
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/html" "$WORK/proxy.d"

FAIL=0
pass() { printf '  ✅ %s\n' "$1"; }
fail() { printf '  ❌ %s\n' "$1"; FAIL=$((FAIL + 1)); }

# 用指定环境变量运行入口脚本，产物落在 $WORK
run_entrypoint() {
  # sed 把镜像内路径改到临时目录，其余逻辑保持原样
  sed -e "s|^CONFIG_JS=.*|CONFIG_JS=$WORK/html/config.js|" \
      -e "s|^PROXY_CONF=.*|PROXY_CONF=$WORK/proxy.d/api.conf|" \
      "$ENTRYPOINT" > "$WORK/run.sh"
  env "$@" sh "$WORK/run.sh" > "$WORK/out.log" 2>&1
}

# 每次运行前恢复"镜像内置的默认 proxy 配置"
reset_proxy() { cp "$ROOT/proxy.d/api.conf" "$WORK/proxy.d/api.conf"; }

echo "== 1. 脚本语法 =="
if sh -n "$ENTRYPOINT"; then pass "sh -n 通过"; else fail "sh -n 失败"; fi

echo "== 2. 未启用反代：生成 JSON 404，且不覆盖用户指定的接口地址 =="
reset_proxy
run_entrypoint LANDRIVE_API_BASE_URL=https://api.example.com/api
if grep -q 'return 404' "$WORK/proxy.d/api.conf"; then
  pass "proxy 配置仍为 JSON 404（/api 不会落到 SPA 回退返回 HTML）"
else
  fail "未反代时 proxy 配置被改成了非 404"
fi
if grep -q 'apiBaseUrl: "https://api.example.com/api"' "$WORK/html/config.js"; then
  pass "运行时接口地址被正确写入 config.js"
else
  fail "config.js 未写入运行时接口地址"
fi
if grep -q 'proxy_pass' "$WORK/proxy.d/api.conf"; then
  fail "未设置 LANDRIVE_API_PROXY 却生成了反代配置"
else
  pass "未设置 LANDRIVE_API_PROXY 时不产生反代"
fi

echo "== 3. 启用反代：/api 反代到后端，前端改用同源 /api =="
reset_proxy
run_entrypoint LANDRIVE_API_PROXY=api:8080
if grep -q 'proxy_pass' "$WORK/proxy.d/api.conf"; then
  pass "生成了反代配置"
else
  fail "启用反代却未生成反代配置"
fi
if grep -q 'api:8080' "$WORK/proxy.d/api.conf"; then
  pass "上游地址正确写入"
else
  fail "上游地址缺失"
fi
# 反代形态下前端必须走同源 /api，否则仍会跨域直连内网，反代白配。
if grep -q 'apiBaseUrl: "/api"' "$WORK/html/config.js"; then
  pass "前端自动改用同源 /api（否则反代不会被使用）"
else
  fail "反代形态下前端未切换到同源 /api"
fi
# 后端晚于前端启动时不能让 nginx 起不来，因此必须是变量 + resolver 的写法。
if grep -q 'proxy_pass \$lanfs_backend' "$WORK/proxy.d/api.conf" && grep -q 'resolver ' "$WORK/proxy.d/api.conf"; then
  pass "使用变量 + resolver（后端未就绪不会导致 nginx 启动失败）"
else
  fail "proxy_pass 直接写了主机名：后端未就绪时 nginx 会启动失败"
fi
# proxy_pass 带结尾斜杠会剥掉 /api 前缀，后端路由全部 404。
if grep -qE 'proxy_pass\s+http://[^;]*/;' "$WORK/proxy.d/api.conf"; then
  fail "proxy_pass 带了结尾斜杠，会剥掉 /api 前缀"
else
  pass "proxy_pass 无结尾斜杠（/api 前缀得以保留）"
fi

echo "== 4. 反代地址各种合法写法都能归一化 =="
for input_and_expect in \
  "api|http://api:8080" \
  "api:9000|http://api:9000" \
  "http://api:8080|http://api:8080" \
  "http://api:8080/|http://api:8080" \
  "lan-drive-api|http://lan-drive-api:8080" \
  "192.168.1.10:9999|http://192.168.1.10:9999"
do
  input=${input_and_expect%%|*}
  expect=${input_and_expect##*|}
  reset_proxy
  run_entrypoint "LANDRIVE_API_PROXY=$input"
  if grep -q "set \$lanfs_backend \"$expect\"" "$WORK/proxy.d/api.conf"; then
    pass "$input → $expect"
  else
    fail "$input 归一化结果不符（期望 $expect）"
  fi
done

echo "== 5. 非法地址必须被拒绝（含注入与换行）=="
# 这些值若被接受，轻则容器起不来，重则把任意指令注入 nginx 配置。
reset_proxy
for bad in \
  'api:8080; } location /evil {' \
  'api:99999' \
  'api:0' \
  'api:abc' \
  'http://a b' \
  'api:8080/path' \
  '-bad' \
  '.bad' \
  'api:' \
  ':8080'
do
  if run_entrypoint "LANDRIVE_API_PROXY=$bad"; then
    fail "非法值被接受：$bad"
  else
    pass "已拒绝：$(printf '%s' "$bad" | cut -c1-34)"
  fi
done
# 换行注入：grep 按行匹配会漏判，必须用 case 做整串匹配。
reset_proxy
if run_entrypoint "LANDRIVE_API_PROXY=api:8080
} location /evil { return 200 'pwned';"; then
  fail "含换行的值绕过了校验（可注入 nginx 指令）"
else
  pass "含换行的注入值被拒绝"
fi

echo "== 6. 可选参数：超时与 resolver =="
reset_proxy
run_entrypoint LANDRIVE_API_PROXY=api:8080 LANDRIVE_API_PROXY_READ_TIMEOUT=1200 LANDRIVE_DNS_RESOLVER=10.0.0.2
if grep -q 'proxy_read_timeout 1200s' "$WORK/proxy.d/api.conf"; then
  pass "读超时可配置"
else
  fail "读超时未生效"
fi
if grep -q 'resolver 10.0.0.2' "$WORK/proxy.d/api.conf"; then
  pass "DNS resolver 可覆盖"
else
  fail "DNS resolver 覆盖未生效"
fi
# 超时给非法值时应回退默认，而不是生成非法 nginx 配置。
reset_proxy
run_entrypoint LANDRIVE_API_PROXY=api:8080 LANDRIVE_API_PROXY_READ_TIMEOUT=abc
if grep -q 'proxy_read_timeout 600s' "$WORK/proxy.d/api.conf"; then
  pass "非法超时回退到默认值"
else
  fail "非法超时未回退"
fi

echo "== 7. 用真实 nginx 校验生成的配置可解析 =="
if command -v nginx >/dev/null 2>&1; then
  NGX=$(mktemp -d)
  trap 'rm -rf "$WORK" "$NGX"' EXIT
  mkdir -p "$NGX/conf.d" "$NGX/logs" "$NGX/html"
  echo '<html>ok</html>' > "$NGX/html/index.html"
  # 复用被测的站点配置，仅把路径/端口指向临时目录
  sed -e "s|include /etc/nginx/proxy.d/\*\.conf;|include $NGX/proxy.d/*.conf;|" \
      -e "s|root /usr/share/nginx/html;|root $NGX/html;|" \
      -e 's|listen 80;|listen 18099;|' "$ROOT/nginx.conf" > "$NGX/conf.d/default.conf"
  cat > "$NGX/nginx.conf" <<EOF
worker_processes 1;
error_log $NGX/logs/error.log warn;
pid $NGX/nginx.pid;
events { worker_connections 32; }
http {
  include /etc/nginx/mime.types;
  access_log $NGX/logs/access.log;
  client_body_temp_path $NGX/logs/body;
  proxy_temp_path $NGX/logs/proxy;
  fastcgi_temp_path $NGX/logs/fastcgi;
  uwsgi_temp_path $NGX/logs/uwsgi;
  scgi_temp_path $NGX/logs/scgi;
  include $NGX/conf.d/*.conf;
}
EOF
  mkdir -p "$NGX/proxy.d"
  # 未反代（默认 JSON 404）
  cp "$ROOT/proxy.d/api.conf" "$NGX/proxy.d/api.conf"
  if nginx -t -c "$NGX/nginx.conf" >/dev/null 2>&1; then
    pass "默认（未反代）配置可被 nginx 解析"
  else
    fail "默认配置无法被 nginx 解析"
  fi
  # 反代：上游主机不存在时也必须能解析（靠变量 + resolver 延迟解析）
  reset_proxy
  run_entrypoint LANDRIVE_API_PROXY=backend-not-exist:8080 LANDRIVE_DNS_RESOLVER=127.0.0.11
  cp "$WORK/proxy.d/api.conf" "$NGX/proxy.d/api.conf"
  if nginx -t -c "$NGX/nginx.conf" >/dev/null 2>&1; then
    pass "上游不存在时配置仍可解析（nginx 不会因后端未就绪而起不来）"
  else
    fail "上游不存在导致配置无法解析（后端未就绪时整个前端会启动失败）"
  fi
else
  printf '  ⚠️  未安装 nginx，跳过配置解析校验（CI 中会执行）\n'
fi

echo
if [ "$FAIL" -eq 0 ]; then
  echo "✅ 前端运行时配置测试全部通过"
else
  echo "❌ 有 $FAIL 项未通过"
  exit 1
fi
