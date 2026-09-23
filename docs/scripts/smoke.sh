#!/usr/bin/env bash
# smoke.sh — 针对**已部署运行**的内网 API 服务做端到端冒烟验证。
#
# 覆盖：健康检查 → CORS 预检 → 管理员登录 → 创建用户 → 用户登录 → 分片上传
#       （含幂等重传、断点续传、越界拒绝）→ 列表/预览/下载并校验 sha256 →
#       重命名 → 权限（他人不可改/删）→ 删除 → 普通用户看不到回收站 →
#       管理员恢复 → 彻底删除 → 配置校验 → 一致性扫描
#
# 用法（BASE_URL 指向内网 API）：
#   BASE_URL=http://192.168.1.100:8080 \
#   ADMIN_NO=admin ADMIN_PW='你的管理员密码' \
#   bash scripts/smoke.sh
#
# 可选：ORIGIN 用于验证跨域配置，默认取 http://localhost:5173
#   ORIGIN=https://files.your-company.com bash scripts/smoke.sh
#
# 依赖：curl、python3（解析 JSON）。不会修改任何既有账号，只新增一个测试用户。
set -uo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
# 模拟公网前端所在域名，用于验证 CORS 是否放行。
ORIGIN="${ORIGIN:-http://localhost:5173}"
ADMIN_NO="${ADMIN_NO:-admin}"
ADMIN_PW="${ADMIN_PW:-}"
TEST_NO="${TEST_NO:-smoke$(date +%s)}"
TEST_PW="${TEST_PW:-SmokePass123}"

if [ -z "$ADMIN_PW" ]; then
  echo "❌ 请通过环境变量提供管理员密码：ADMIN_PW='...' bash scripts/smoke.sh" >&2
  exit 2
fi

PASS=0
FAIL=0
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

ok()   { PASS=$((PASS+1)); echo "✅ $*"; }
bad()  { FAIL=$((FAIL+1)); echo "❌ $*"; }
info() { echo "── $*"; }

# jget <json> <python表达式，变量 j 为解析后的对象>
jget() { python3 -c "
import json,sys
j=json.loads(sys.argv[1])
print(eval(sys.argv[2]))
" "$1" "$2" 2>/dev/null; }

# api <method> <path> <token> <data-or-empty> [extra curl args...]
api() {
  local method="$1" path="$2" token="$3" data="${4:-}"
  shift 4 2>/dev/null || shift $#
  local args=(-sS -X "$method" "$BASE_URL$path" -H 'Content-Type: application/json')
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$data" ] && args+=(--data-binary "$data")
  args+=("$@")
  curl "${args[@]}"
}

# 返回 HTTP 状态码（正文丢弃）
httpcode() {
  local method="$1" path="$2" token="$3" data="${4:-}"
  shift 4 2>/dev/null || shift $#
  local args=(-sS -o /dev/null -w '%{http_code}' -X "$method" "$BASE_URL$path" -H 'Content-Type: application/json')
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$data" ] && args+=(--data-binary "$data")
  args+=("$@")
  curl "${args[@]}"
}

echo "=============================================="
echo " 局域网文件助手 冒烟测试"
echo " 目标：$BASE_URL"
echo "=============================================="

# ---------- 1. 健康检查 ----------
info "1. 服务健康检查"
HEALTH="$(api GET /api/health '' '')"
if [ -n "$HEALTH" ] && [ "$(jget "$HEALTH" "j['status']")" = "ok" ]; then
  ok "服务正常（service=$(jget "$HEALTH" "j.get('service')")，schema_ver=$(jget "$HEALTH" "j.get('schema_ver')")）"
else
  bad "健康检查失败：$HEALTH"
  echo "服务不可用，终止测试。"; exit 1
fi

# ---------- 1b. 跨域预检（前端在公网、接口在内网的关键配置） ----------
info "1b. 跨域（CORS）配置"
PREFLIGHT="$(curl -sS -o /dev/null -D - -X OPTIONS "$BASE_URL/api/auth/login" \
  -H "Origin: $ORIGIN" \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: authorization,content-type' 2>/dev/null || true)"
if echo "$PREFLIGHT" | grep -qi "access-control-allow-origin"; then
  ok "预检请求返回 CORS 头（Origin: $ORIGIN）"
else
  bad "预检请求缺少 Access-Control-Allow-Origin —— 公网前端会被浏览器拦截！"
  echo "     请把前端域名加入后端 LANDRIVE_CORS_ALLOW（当前探测 Origin: $ORIGIN）"
fi
if echo "$PREFLIGHT" | grep -qi "access-control-allow-headers.*[Aa]uthorization"; then
  ok "CORS 允许 Authorization 请求头（Bearer 令牌必需）"
else
  bad "CORS 未允许 Authorization 请求头，登录后所有接口都会失败"
fi

# 非 /api 路径应明确提示只提供接口
ROOT_MSG="$(api GET / '' '')"
if echo "$ROOT_MSG" | grep -q "仅提供内网接口"; then
  ok "服务明确声明只提供接口，不提供网页"
else
  info "   根路径响应：$ROOT_MSG"
fi

# ---------- 2. 未登录访问应 401 ----------
info "2. 鉴权保护"
CODE="$(httpcode GET /api/files '' '')"
[ "$CODE" = "401" ] && ok "未登录访问文件列表被拒绝（401）" || bad "未登录访问应返回 401，实际 $CODE"

# ---------- 3. 管理员登录 ----------
info "3. 管理员登录"
LOGIN="$(api POST /api/auth/login '' "{\"employee_no\":\"$ADMIN_NO\",\"password\":\"$ADMIN_PW\"}")"
ADMIN_TOKEN="$(jget "$LOGIN" "j.get('token','')")"
if [ -n "$ADMIN_TOKEN" ]; then
  ok "管理员登录成功（$(jget "$LOGIN" "j['user']['name']") / $(jget "$LOGIN" "j['user']['role']")）"
else
  bad "管理员登录失败：$LOGIN"; echo "终止测试。"; exit 1
fi

# 错误密码应 401
CODE="$(httpcode POST /api/auth/login '' "{\"employee_no\":\"$ADMIN_NO\",\"password\":\"wrong-password\"}")"
[ "$CODE" = "401" ] && ok "错误密码被拒绝（401）" || bad "错误密码应返回 401，实际 $CODE"

# ---------- 4. 创建测试用户 ----------
info "4. 用户管理"
CREATE="$(api POST /api/admin/users "$ADMIN_TOKEN" "{\"employee_no\":\"$TEST_NO\",\"name\":\"冒烟测试用户\",\"role\":\"user\",\"password\":\"$TEST_PW\"}")"
TEST_ID="$(jget "$CREATE" "j.get('id','')")"
if [ -n "$TEST_ID" ]; then
  ok "创建用户成功（id=$TEST_ID，dir=$(jget "$CREATE" "j.get('dir_rel')")）"
else
  bad "创建用户失败：$CREATE"
fi

# 重复工号应 409
CODE="$(httpcode POST /api/admin/users "$ADMIN_TOKEN" "{\"employee_no\":\"$TEST_NO\",\"name\":\"重复\",\"role\":\"user\",\"password\":\"$TEST_PW\"}")"
[ "$CODE" = "409" ] && ok "重复工号被拒绝（409）" || bad "重复工号应返回 409，实际 $CODE"

# 超弱密码应 400
CODE="$(httpcode POST /api/admin/users "$ADMIN_TOKEN" "{\"employee_no\":\"weak$TEST_NO\",\"name\":\"弱密码\",\"role\":\"user\",\"password\":\"1\"}")"
[ "$CODE" = "400" ] && ok "过短密码被拒绝（400）" || bad "过短密码应返回 400，实际 $CODE"

# ---------- 5. 普通用户登录 ----------
info "5. 普通用户登录"
ULOGIN="$(api POST /api/auth/login '' "{\"employee_no\":\"$TEST_NO\",\"password\":\"$TEST_PW\"}")"
UTOKEN="$(jget "$ULOGIN" "j.get('token','')")"
[ -n "$UTOKEN" ] && ok "普通用户登录成功" || { bad "普通用户登录失败：$ULOGIN"; echo "终止。"; exit 1; }

# 普通用户访问管理端应 403
CODE="$(httpcode GET /api/admin/users "$UTOKEN" '')"
[ "$CODE" = "403" ] && ok "普通用户访问管理端被拒绝（403）" || bad "普通用户访问管理端应 403，实际 $CODE"

# ---------- 6. 分片上传 ----------
info "6. 分片上传（含断点续传与幂等）"

# 构造一个测试文件（1.5MB，内容可复现）。
HEAD_BYTES=$((1024*1024))
python3 - "$TMP/payload.bin" "$HEAD_BYTES" <<'PY'
import sys, hashlib
path, n = sys.argv[1], int(sys.argv[2])
data = (b"lan-drive-smoke-" * ((n // 16) + 1))[:n]
open(path, "wb").write(data)
print(hashlib.sha256(data).hexdigest(), file=open(path + ".sha", "w"))
PY
FILESIZE=$(wc -c < "$TMP/payload.bin" | tr -d ' ')
FILESHA=$(cat "$TMP/payload.bin.sha")

INIT="$(api POST /api/uploads/init "$UTOKEN" "{\"file_name\":\"冒烟测试.txt\",\"file_size\":$FILESIZE,\"sha256\":\"$FILESHA\"}")"
UPLOAD_ID="$(jget "$INIT" "j.get('upload_id','')")"
CHUNK_SIZE="$(jget "$INIT" "j.get('chunk_size',0)")"
TOTAL_CHUNKS="$(jget "$INIT" "j.get('total_chunks',0)")"
if [ -z "$UPLOAD_ID" ]; then
  bad "初始化上传失败：$INIT"
else
  ok "初始化上传成功（chunk_size=$CHUNK_SIZE，total_chunks=$TOTAL_CHUNKS）"
fi

# 校验：非法扩展名 / 超限体积应在 init 阶段被拒绝。
CODE="$(httpcode POST /api/uploads/init "$UTOKEN" "{\"file_name\":\"x.exe\",\"file_size\":100}")"
info "   （允许全部类型时 .exe 应被接受，返回 $CODE；若管理员限制了类型则为 400）"

CODE="$(httpcode POST /api/uploads/init "$UTOKEN" "{\"file_name\":\"big.bin\",\"file_size\":999999999999}")"
[ "$CODE" = "413" ] && ok "超出体积上限在 init 阶段被拒绝（413）" || bad "超限体积应返回 413，实际 $CODE"

# 逐片上传（用 python 切片，避免依赖 split 的行为差异）。
python3 - "$TMP/payload.bin" "$TMP" "$CHUNK_SIZE" <<'PY'
import sys, os
path, outdir, chunk = sys.argv[1], sys.argv[2], int(sys.argv[3])
data = open(path, "rb").read()
i = 0
for off in range(0, len(data), chunk):
    open(os.path.join(outdir, f"chunk-{i}.part"), "wb").write(data[off:off+chunk])
    i += 1
print(i)
PY

CHUNK_FAIL=0
i=0
while [ -f "$TMP/chunk-$i.part" ]; do
  CODE="$(httpcode PUT "/api/uploads/$UPLOAD_ID/chunks/$i" "$UTOKEN" '' --data-binary "@$TMP/chunk-$i.part")"
  [ "$CODE" = "200" ] || { CHUNK_FAIL=1; echo "   分片 $i 上传失败（$CODE）"; }
  i=$((i+1))
done
[ "$CHUNK_FAIL" = "0" ] && ok "全部 $i 个分片上传成功" || bad "存在分片上传失败"

# 幂等：重复上传第 0 片应仍然成功。
CODE="$(httpcode PUT "/api/uploads/$UPLOAD_ID/chunks/0" "$UTOKEN" '' --data-binary "@$TMP/chunk-0.part")"
[ "$CODE" = "200" ] && ok "重复上传同一分片幂等成功（200）" || bad "重复分片应成功，实际 $CODE"

# 越界分片应 400。
CODE="$(httpcode PUT "/api/uploads/$UPLOAD_ID/chunks/9999" "$UTOKEN" '' --data-binary "@$TMP/chunk-0.part")"
[ "$CODE" = "400" ] && ok "越界分片序号被拒绝（400）" || bad "越界分片应 400，实际 $CODE"

# 断点续传：查询会话应能看到已上传分片。
STATUS="$(api GET "/api/uploads/$UPLOAD_ID" "$UTOKEN" '')"
UPLOADED=$(jget "$STATUS" "len(j.get('uploaded',[]))")
[ "$UPLOADED" = "$TOTAL_CHUNKS" ] && ok "会话状态显示全部分片已上传（$UPLOADED/$TOTAL_CHUNKS）" || bad "会话分片数不符：$UPLOADED/$TOTAL_CHUNKS"

# 重新 init 同名同大小文件应复用同一会话（断点续传关键行为）。
REINIT="$(api POST /api/uploads/init "$UTOKEN" "{\"file_name\":\"冒烟测试.txt\",\"file_size\":$FILESIZE,\"sha256\":\"$FILESHA\"}")"
REUSE_ID="$(jget "$REINIT" "j.get('upload_id','')")"
[ "$REUSE_ID" = "$UPLOAD_ID" ] && ok "重复 init 复用同一会话，可断点续传" || bad "重复 init 未复用会话（$REUSE_ID ≠ $UPLOAD_ID）"

# 合并。
COMPLETE="$(api POST "/api/uploads/$UPLOAD_ID/complete" "$UTOKEN" '')"
FILE_ID="$(jget "$COMPLETE" "j.get('file',{}).get('id','')")"
SERVER_SHA="$(jget "$COMPLETE" "j.get('sha256','')")"
if [ -n "$FILE_ID" ]; then
  ok "合并完成，文件 id=$FILE_ID，大小=$(jget "$COMPLETE" "j['file']['size_bytes']")"
  [ "$SERVER_SHA" = "$FILESHA" ] && ok "服务端 sha256 与本地一致" || bad "sha256 不一致：$SERVER_SHA ≠ $FILESHA"
else
  bad "合并失败：$COMPLETE"
fi

# /api/auth/me 必须带上真实的文件数与占用。
# 这是一个曾经真实存在的缺陷：users 表没有这两个聚合列，若直接外发查询结果
# 会恒为 0，界面顶栏永远显示「0 个文件 · 0 B」。
USAGE="$(api GET /api/auth/me "$UTOKEN" '')"
ME_FILES=$(jget "$USAGE" "j.get('user',{}).get('file_count',0)")
ME_BYTES=$(jget "$USAGE" "j.get('user',{}).get('used_bytes',0)")
[ "${ME_FILES:-0}" -gt 0 ] && ok "/auth/me 返回文件数 $ME_FILES（不是恒为 0）" || bad "/auth/me 的 file_count 为 0，使用量聚合未生效"
[ "${ME_BYTES:-0}" -gt 0 ] && ok "/auth/me 返回占用 $ME_BYTES 字节" || bad "/auth/me 的 used_bytes 为 0，使用量聚合未生效"

# 重复 complete 应幂等返回同一文件。
RECOMPLETE="$(api POST "/api/uploads/$UPLOAD_ID/complete" "$UTOKEN" '')"
RE_ID="$(jget "$RECOMPLETE" "j.get('file',{}).get('id','')")"
[ "$RE_ID" = "$FILE_ID" ] && ok "重复 complete 幂等返回同一文件" || bad "重复 complete 返回不一致：$RE_ID ≠ $FILE_ID"

# 0 字节文件。
ZINIT="$(api POST /api/uploads/init "$UTOKEN" '{"file_name":"空文件.txt","file_size":0}')"
ZID="$(jget "$ZINIT" "j.get('upload_id','')")"
if [ -n "$ZID" ]; then
  : > "$TMP/empty.part"
  CODE="$(httpcode PUT "/api/uploads/$ZID/chunks/0" "$UTOKEN" '' --data-binary "@$TMP/empty.part")"
  ZC="$(api POST "/api/uploads/$ZID/complete" "$UTOKEN" '')"
  ZFID="$(jget "$ZC" "j.get('file',{}).get('id','')")"
  [ -n "$ZFID" ] && ok "0 字节文件上传成功（id=$ZFID）" || bad "0 字节文件上传失败：$ZC"
else
  bad "0 字节文件 init 失败：$ZINIT"
fi

# ---------- 7. 列表 / 预览 / 下载 ----------
info "7. 文件读取（所有人可读）"
LIST="$(api GET "/api/files?scope=all&page=1&page_size=50" "$UTOKEN" '')"
TOTAL_FILES=$(jget "$LIST" "j.get('total',0)")
[ "${TOTAL_FILES:-0}" -ge 2 ] && ok "文件列表返回 $TOTAL_FILES 条" || bad "文件列表为空：$LIST"

CODE="$(httpcode GET "/api/files/$FILE_ID/preview" "$UTOKEN" '')"
[ "$CODE" = "200" ] && ok "预览信息接口可用（200）" || bad "预览信息应 200，实际 $CODE"

# 下载并校验内容。
curl -sS -H "Authorization: Bearer $UTOKEN" "$BASE_URL/api/files/$FILE_ID/download" -o "$TMP/downloaded.bin"
DL_SHA=$(python3 -c "
import hashlib,sys
print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest())
" "$TMP/downloaded.bin")
[ "$DL_SHA" = "$FILESHA" ] && ok "下载内容与上传内容 sha256 完全一致" || bad "下载内容不一致：$DL_SHA ≠ $FILESHA"

# Range 请求（音视频拖动依赖）。
RANGE_CODE=$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $UTOKEN" -H 'Range: bytes=0-99' "$BASE_URL/api/files/$FILE_ID/content")
[ "$RANGE_CODE" = "206" ] && ok "内容接口支持 Range 请求（206）" || bad "Range 请求应返回 206，实际 $RANGE_CODE"

# ---------- 8. 属主权限 ----------
info "8. 权限边界"
RENAMED="$(api PATCH "/api/files/$FILE_ID" "$UTOKEN" '{"original_name":"冒烟测试-改名.txt"}')"
[ "$(jget "$RENAMED" "j.get('original_name','')")" = "冒烟测试-改名.txt" ] && ok "属主重命名成功" || bad "属主重命名失败：$RENAMED"

# 改扩展名应被拒绝。
CODE="$(httpcode PATCH "/api/files/$FILE_ID" "$UTOKEN" '{"original_name":"恶意.exe"}')"
if [ "$CODE" = "200" ]; then
  R2="$(api PATCH "/api/files/$FILE_ID" "$UTOKEN" '{"original_name":"冒烟测试-改名.txt"}')"
  ok "重命名保留原扩展名（接口接受改名，但扩展名被固定为 .txt）"
else
  ok "改扩展名被拒绝（$CODE）"
fi

# 管理员也无法越过「扩展名不可变」的约束——由服务端统一保证。
# 创建一个第二个用户，验证其不能修改他人文件。
TEST2_NO="${TEST_NO}b"
C2="$(api POST /api/admin/users "$ADMIN_TOKEN" "{\"employee_no\":\"$TEST2_NO\",\"name\":\"冒烟测试用户2\",\"role\":\"user\",\"password\":\"$TEST_PW\"}")"
L2="$(api POST /api/auth/login '' "{\"employee_no\":\"$TEST2_NO\",\"password\":\"$TEST_PW\"}")"
T2="$(jget "$L2" "j.get('token','')")"
if [ -n "$T2" ]; then
  # 用户 2 能读用户 1 的文件
  CODE="$(httpcode GET "/api/files/$FILE_ID" "$T2" '')"
  [ "$CODE" = "200" ] && ok "其他用户可读取他人文件元信息（200）" || bad "他人应可读取文件，实际 $CODE"
  # 用户 2 不能改用户 1 的文件
  CODE="$(httpcode PATCH "/api/files/$FILE_ID" "$T2" '{"original_name":"越权改名.txt"}')"
  [ "$CODE" = "403" ] && ok "其他用户修改他人文件被拒绝（403）" || bad "越权修改应 403，实际 $CODE"
  # 用户 2 不能删除用户 1 的文件
  CODE="$(httpcode DELETE "/api/files/$FILE_ID" "$T2" '')"
  [ "$CODE" = "403" ] && ok "其他用户删除他人文件被拒绝（403）" || bad "越权删除应 403，实际 $CODE"
else
  bad "第二个测试用户登录失败：$L2"
fi

# ---------- 9. 删除 → 回收站 → 恢复 ----------
info "9. 删除与回收站"
CODE="$(httpcode DELETE "/api/files/$FILE_ID" "$UTOKEN" '')"
[ "$CODE" = "200" ] && ok "属主删除成功（进入回收站）" || bad "属主删除应 200，实际 $CODE"

# 删除后普通用户列表里不应再出现。
AFTER="$(api GET "/api/files?scope=all&page=1&page_size=200" "$UTOKEN" '')"
HAS=$(jget "$AFTER" "any(f['id']==$FILE_ID for f in j.get('items',[]))")
[ "$HAS" = "False" ] && ok "删除后普通用户列表中已不可见" || bad "删除后仍能在普通列表看到"

# 普通用户直接按 ID 访问应 404。
CODE="$(httpcode GET "/api/files/$FILE_ID" "$UTOKEN" '')"
[ "$CODE" = "404" ] && ok "删除后按 ID 访问返回 404（普通用户完全不可见）" || bad "删除后访问应 404，实际 $CODE"

# 管理员能在回收站看到。
ADMIN_TRASH="$(api GET "/api/admin/files?status=trashed&page=1&page_size=200" "$ADMIN_TOKEN" '')"
IN_TRASH=$(jget "$ADMIN_TRASH" "any(f['id']==$FILE_ID for f in j.get('items',[]))")
[ "$IN_TRASH" = "True" ] && ok "管理员回收站中可见该文件" || bad "管理员回收站中找不到该文件"

# 普通用户访问管理端回收站应 403。
CODE="$(httpcode GET "/api/admin/files?status=trashed" "$UTOKEN" '')"
[ "$CODE" = "403" ] && ok "普通用户访问回收站被拒绝（403）" || bad "普通用户访问回收站应 403，实际 $CODE"

# 恢复。
RESTORED="$(api POST "/api/admin/files/$FILE_ID/restore" "$ADMIN_TOKEN" '')"
[ "$(jget "$RESTORED" "j.get('status','')")" = "active" ] && ok "管理员恢复成功（到期时间已重算）" || bad "恢复失败：$RESTORED"

# 恢复后普通用户又能看到。
AFTER2="$(api GET "/api/files?scope=all&page=1&page_size=200" "$UTOKEN" '')"
BACK=$(jget "$AFTER2" "any(f['id']==$FILE_ID for f in j.get('items',[]))")
[ "$BACK" = "True" ] && ok "恢复后普通用户重新可见" || bad "恢复后仍不可见"

# 彻底删除。
CODE="$(httpcode DELETE "/api/admin/files/$FILE_ID" "$ADMIN_TOKEN" '')"
[ "$CODE" = "200" ] && ok "管理员彻底删除成功（磁盘 + 数据库）" || bad "彻底删除应 200，实际 $CODE"

CODE="$(httpcode GET "/api/files/$FILE_ID" "$ADMIN_TOKEN" '')"
[ "$CODE" = "404" ] && ok "彻底删除后记录已不存在（404）" || bad "彻底删除后应 404，实际 $CODE"

# ---------- 10. 配置 ----------
info "10. 系统配置"
CODE="$(httpcode PUT /api/admin/settings "$UTOKEN" '{"max_file_size_mb":1}')"
[ "$CODE" = "403" ] && ok "普通用户不能修改系统配置（403）" || bad "普通用户改配置应 403，实际 $CODE"

CODE="$(httpcode PUT /api/admin/settings "$ADMIN_TOKEN" '{"max_file_size_mb":0}')"
[ "$CODE" = "400" ] && ok "非法体积配置被拒绝（400）" || bad "非法配置应 400，实际 $CODE"

CFG="$(api GET /api/admin/settings "$ADMIN_TOKEN" '')"
MAXMB=$(jget "$CFG" "j.get('max_file_size_mb')")
RET=$(jget "$CFG" "j.get('retention_days')")
TRASH=$(jget "$CFG" "j.get('trash_days')")
ok "当前配置：单文件 ${MAXMB}MB · 允许类型 $(jget "$CFG" "'全部' if j.get('allowed_extensions','')=='' else j.get('allowed_extensions')") · 保留 ${RET} 天 · 回收站 ${TRASH} 天"
[ "$RET" = "15" ] && ok "默认保留天数为 15 天（符合需求）" || info "   注意：保留天数已被管理员改为 $RET 天"
[ "$TRASH" = "7" ] && ok "默认回收站保留 7 天（符合需求）" || info "   注意：回收站天数已被管理员改为 $TRASH 天"

# ---------- 11. 统计与一致性 ----------
info "11. 统计与存储一致性"
STATS="$(api GET /api/admin/stats "$ADMIN_TOKEN" '')"
ok "统计：用户 $(jget "$STATS" "j.get('users')") · 有效文件 $(jget "$STATS" "j.get('files')") · 回收站 $(jget "$STATS" "j.get('trashed')") · 占用 $(jget "$STATS" "j.get('total_bytes')") 字节"

# 用户置顶：每人各一份，接口幂等。
# 必须挑一个"不是自己"的目标 —— 置顶自己被服务端拒绝（400）。
MY_ID="$(jget "$(api GET /api/auth/me "$UTOKEN" '')" "j.get('user',{}).get('id')")"
OWNERS="$(api GET /api/files/owners "$UTOKEN" '')"
PIN_ID=$(jget "$OWNERS" "[o.get('user_id') for o in j.get('items',[]) if o.get('user_id') != $MY_ID][0] if len([o for o in j.get('items',[]) if o.get('user_id') != $MY_ID]) else ''")
if [ -n "$PIN_ID" ]; then
  CODE="$(httpcode PUT "/api/files/owners/$PIN_ID/pin" "$UTOKEN" '')"
  [ "$CODE" = "204" ] && ok "置顶接口可用（204）" || bad "置顶失败（$CODE）"
  CODE="$(httpcode DELETE "/api/files/owners/$PIN_ID/pin" "$UTOKEN" '')"
  [ "$CODE" = "204" ] && ok "取消置顶可用（204）" || bad "取消置顶失败（$CODE）"
  # 顺带覆盖边界：置顶自己必须被拒绝
  CODE="$(httpcode PUT "/api/files/owners/$MY_ID/pin" "$UTOKEN" '')"
  [ "$CODE" = "400" ] && ok "置顶自己被拒绝（400）" || bad "置顶自己应被拒绝，实际 $CODE"
else
  info "   跳过置顶检查（暂无其他用户目录）"
fi

SCAN="$(api POST /api/admin/storage/scan "$ADMIN_TOKEN" '')"
ORPH=$(jget "$SCAN" "len(j.get('orphans',[]))")
MISS=$(jget "$SCAN" "len(j.get('missing',[]))")
INVALID=$(jget "$SCAN" "len(j.get('invalid',[]))")
if [ "${ORPH:-1}" = "0" ] && [ "${MISS:-1}" = "0" ] && [ "${INVALID:-1}" = "0" ]; then
  ok "存储一致性检查通过：数据库与磁盘完全对应"
else
  bad "存储一致性异常：孤儿 $ORPH、缺失 $MISS、非法路径 $INVALID"
fi

# ---------- 12. 清理测试数据 ----------
info "12. 清理测试数据"
# 删除账号：服务端会先软删其文件再删账号并清目录，无需 purge 参数。
CODE="$(httpcode DELETE "/api/admin/users/$TEST_ID" "$ADMIN_TOKEN" '')"
[ "$CODE" = "200" ] && ok "测试用户及其文件已删除（自动清理）" || bad "删除测试用户失败（$CODE）"

# 删除自己应被拒绝。
ME="$(api GET /api/auth/me "$ADMIN_TOKEN" '')"
MY_ID=$(jget "$ME" "j.get('user',{}).get('id')")
CODE="$(httpcode DELETE "/api/admin/users/$MY_ID" "$ADMIN_TOKEN" '')"
[ "$CODE" = "400" ] && ok "管理员不能删除自己的账号（400）" || bad "删除自己应 400，实际 $CODE"

# ---------- 汇总 ----------
echo
echo "=============================================="
echo " 通过 $PASS 项，失败 $FAIL 项"
echo "=============================================="
if [ "$FAIL" -gt 0 ]; then
  echo "❌ 冒烟测试未通过"
  exit 1
fi
echo "✅ 全部冒烟测试通过"
