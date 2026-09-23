// API 基址解析的单元测试（Node 环境直接运行，零依赖）。
//
// 运行：node --experimental-strip-types src/api/base-url.spec.ts
//
// 重点覆盖「前端镜像内 nginx 把 /api 反代到后端容器」这一新部署形态：
// 此时运行时 /config.js 会写入 apiBaseUrl="/api"，必须**压过**构建期注入的后端域名，
// 否则浏览器仍会跨域直连内网，反代形同虚设。
import assert from 'node:assert/strict'
import {
  isCrossOriginBaseUrl,
  resolveApiBaseUrl,
  resolveProbeTimeout
} from './base-url.ts'

const BUILD_HOST = 'https://api.example.com'

// 1) 运行时显式绝对地址优先于构建期 HOST
assert.equal(
  resolveApiBaseUrl('https://api.example.com/api', BUILD_HOST),
  'https://api.example.com/api',
  '运行时绝对地址应优先'
)

// 2) 运行时同源 "/api"（前端镜像启用 nginx 反代）必须压过构建期 HOST
assert.equal(
  resolveApiBaseUrl('/api', BUILD_HOST),
  '/api',
  '运行时同源 /api 必须优先于构建期 HOST（否则反代不会被使用）'
)

// 3) 运行时为空 -> 回退到构建期 HOST，拼出 <HOST>/api
assert.equal(resolveApiBaseUrl('', BUILD_HOST), `${BUILD_HOST}/api`)
assert.equal(resolveApiBaseUrl(undefined, BUILD_HOST), `${BUILD_HOST}/api`)
assert.equal(resolveApiBaseUrl('   ', BUILD_HOST), `${BUILD_HOST}/api`)

// 4) 都没有 -> 同源 /api
assert.equal(resolveApiBaseUrl('', ''), '/api')
assert.equal(resolveApiBaseUrl(undefined, ''), '/api')

// 5) 结尾斜杠被去掉，避免 //api
assert.equal(resolveApiBaseUrl('https://api.example.com/api///', ''), 'https://api.example.com/api')
assert.equal(resolveApiBaseUrl('', `${BUILD_HOST}/`), `${BUILD_HOST}/api`)

// 6) 跨域判定：只有绝对地址算跨域，同源 /api 不算
assert.equal(isCrossOriginBaseUrl('/api'), false, '同源 /api 不应判为跨域')
assert.equal(isCrossOriginBaseUrl('https://api.example.com/api'), true)
assert.equal(isCrossOriginBaseUrl('http://192.168.1.100/api'), true)

// 7) 探测超时：运行时 > 构建期 > 默认；非法值回退
assert.equal(resolveProbeTimeout(3000, 6000), 3000)
assert.equal(resolveProbeTimeout(undefined, 5000), 5000)
assert.equal(resolveProbeTimeout(undefined, undefined), 6000)
assert.equal(resolveProbeTimeout(0, 0), 6000, '0 不是合法超时，应回退默认')
assert.equal(resolveProbeTimeout(Number.NaN, 'abc'), 6000)
assert.equal(resolveProbeTimeout(undefined, '8000'), 8000, '字符串数字应被接受')

console.log('✅ API 基址解析测试全部通过（含 nginx 反代同源优先）')
