// 健康探测判定的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/api/health.spec.ts
import assert from 'node:assert/strict'
import { isApiHealthPayload, isApiReachable, looksLikeJson } from './health.ts'

const OK_BODY = { status: 'ok', service: 'lan-drive-api', uptime_s: 12 }

// 1) 正常响应 -> 可达
assert.equal(isApiReachable({ status: 200, data: OK_BODY }), true)
// 数据库故障时后端返回 503 + status=degraded：服务在世，仍算可达（不是网络问题）
assert.equal(
  isApiReachable({ status: 503, data: { status: 'degraded', service: 'lan-drive-api' } }),
  true,
  '503 且带 API 载荷说明服务在世，应算可达'
)

// 2) 关键回归：SPA 回退把 /api/health 当成前端路由，返回 200 + HTML
assert.equal(
  isApiReachable({ status: 200, contentType: 'text/html', data: '<!DOCTYPE html><html>...' }),
  false,
  '200 + HTML 是 SPA 兜底页，绝不能判为已连通'
)
assert.equal(isApiReachable({ status: 200, contentType: 'text/html', data: undefined }), false)

// 3) 反代未配置 / 上游不可达时的网关错误
assert.equal(isApiReachable({ status: 502, contentType: 'text/html', data: '<html>502</html>' }), false)
assert.equal(isApiReachable({ status: 502, contentType: 'application/json', data: { error: 'bad gateway' } }), false)
assert.equal(isApiReachable({ status: 404, contentType: 'application/json', data: { error: 'not found' } }), false)

// 4) 没有任何响应信息（请求根本没发出去）-> 不可达
assert.equal(isApiReachable(undefined), false)
assert.equal(isApiReachable({}), false)

// 5) 载荷判定细节
assert.equal(isApiHealthPayload({ status: 'ok' }), true)
assert.equal(isApiHealthPayload({ service: 'lan-drive-api' }), true)
assert.equal(isApiHealthPayload('ok'), false)
assert.equal(isApiHealthPayload(null), false)
assert.equal(isApiHealthPayload(['ok']), false, '数组不是 API 载荷')
assert.equal(isApiHealthPayload({}), false)

// 6) content-type 辅助判定
assert.equal(looksLikeJson('application/json; charset=utf-8'), true)
assert.equal(looksLikeJson('text/html'), false)
assert.equal(looksLikeJson(undefined), false)

console.log('✅ 健康探测判定测试全部通过（SPA 兜底页不会被误判为已连通）')
