#!/bin/sh
# 校验前端构建期输入里没有真实内网域名 / 内网 IP。
#
# 为什么需要：web 镜像推到公开的 ghcr，任何人都能 docker save 解包读取。
# 曾因构建期 --build-arg HOST=<内网域名> 把真实域名烘进前端产物，
# 任何人都能从镜像里读出内网地址。
#
# 只扫**会进入产物**的输入（.env*、vite 配置、src 源码）；
# 不扫文档与测试 —— 测试里用 192.168.x 造跨域样例是正常的，扫它们只会误报。
#
# 用法：sh web/scripts/check-no-internal-host.sh [目录，默认 web/]
set -eu

WEB_DIR=${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}
cd "$WEB_DIR"

# 允许 example.com / localhost 等占位；禁止真实私有域与内网网段。
PATTERN='(https?://[A-Za-z0-9.-]*(local|internal|lan|corp)[A-Za-z0-9.-]*)|(https?://(10|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.)'

# 测试文件不进产物，其中的 192.168.x 是用来验证跨域判定的样例，排除掉。
# （.env.example 是占位模板，一并排除：它不会被 vite 读取为生产配置。）
# 用 --exclude 按**文件名**排除：grep -rn 的输出是 "文件:行:内容"，
# 直接对结果行用 $ 锚定文件名会匹配到内容末尾，必须交给 grep 自己过滤。
FOUND=0
for f in .env .env.production .env.local .env.development vite.config.ts src; do
  [ -e "$f" ] || continue
  if grep -rnE "$PATTERN" "$f" 2>/dev/null \
       --exclude='*.spec.ts' --exclude='*.spec.js' \
       --exclude='*.test.ts' --exclude='*.test.js' \
       --exclude-dir=node_modules; then
    FOUND=1
  fi
done

if [ "$FOUND" = 1 ]; then
  echo "❌ 构建期输入中出现疑似真实内网域名，会被烘进公开镜像" >&2
  echo "   请改用 example.com 之类的占位，实际地址用运行时环境变量注入。" >&2
  exit 1
fi
echo "✅ 构建期输入未发现真实内网域名"
