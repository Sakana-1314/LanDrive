// 网络可用性判定的单元测试（node 环境直接运行，无需浏览器）。
//
// 运行：node --experimental-strip-types src/api/network.spec.ts
// 或通过 vite/vitest（本项目未引入测试框架，这里用 Node 内置断言保持零依赖）。
import assert from 'node:assert/strict'
import {
  NETWORK_UNAVAILABLE_MESSAGE,
  isNetworkError,
  looksLikeNetworkFailure,
  networkError
} from './network.ts'

// 1) 文案必须与需求完全一致（改动会被这里拦住）
assert.equal(NETWORK_UNAVAILABLE_MESSAGE, '无法在此网络下使用，请更换网络再试！')

// 2) 网络层失败的各家错误码都应被识别
for (const code of ['ECONNABORTED', 'ETIMEDOUT', 'ERR_NETWORK', 'ERR_CANCELED']) {
  assert.equal(looksLikeNetworkFailure({ code }), true, `code=${code} 应判为网络失败`)
}
for (const message of ['timeout of 6000ms exceeded', 'Network Error', 'Failed to fetch']) {
  assert.equal(looksLikeNetworkFailure({ message }), true, `message=${message} 应判为网络失败`)
}
// 无 code 无 message 但也没有 response（浏览器 CORS 拦截的典型形态）
assert.equal(looksLikeNetworkFailure({}), true)

// 3) 有 response = 请求到达了服务器 = 网络正常（含 4xx/5xx）
assert.equal(looksLikeNetworkFailure({ response: { status: 401 } }), false)
assert.equal(looksLikeNetworkFailure({ response: { status: 403 } }), false)
assert.equal(looksLikeNetworkFailure({ response: { status: 500 } }), false)
assert.equal(looksLikeNetworkFailure({ response: { status: 200 } }), false)

// 4) networkError 携带标记与固定文案
const err = networkError(new Error('underlying'))
assert.equal(err.message, NETWORK_UNAVAILABLE_MESSAGE)
assert.equal(isNetworkError(err), true)

// 5) 普通业务错误不应被误判为网络错误
const biz = new Error('工号或密码错误') as Error & { status?: number }
biz.status = 401
assert.equal(isNetworkError(biz), false)

console.log('✅ 网络判定测试全部通过（文案与判定规则符合需求）')
