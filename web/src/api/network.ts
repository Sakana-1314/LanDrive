// 网络可用性判定：区分「当前网络访问不到内网接口」与「业务错误」。
//
// 背景：前端部署在公网，后端 API 只在公司内网可达。
// 员工在家中/外网打开页面时，请求会超时或被 CORS 拦截，
// 此时必须给出明确提示：「无法在此网络下使用，请更换网络再试！」
//
// 判定要点：
//   - 请求根本没到达服务器（网络错误 / 超时 / 跨域被拦截）→ 网络不可用；
//   - 服务器返回了 5xx 但服务在世 → 不是网络问题，是服务端故障；
//   - 服务器返回 4xx（含 401/403）→ 请求已到达，网络正常。

/** 统一的网络不可用提示文案（需求指定，不要随意改动）。 */
export const NETWORK_UNAVAILABLE_MESSAGE = '无法在此网络下使用，请更换网络再试！'

/** 携带网络判定信息的错误对象。 */
export interface ApiError extends Error {
  /** HTTP 状态码；网络层失败时为 undefined */
  status?: number
  /** 响应体（若有） */
  data?: unknown
  /** 是否为「当前网络无法访问内网接口」 */
  networkUnavailable?: boolean
}

/** 构造一个网络不可用错误。 */
export function networkError(cause?: unknown): ApiError {
  const err = new Error(NETWORK_UNAVAILABLE_MESSAGE) as ApiError
  err.networkUnavailable = true
  if (cause) (err as ApiError & { cause?: unknown }).cause = cause
  return err
}

/** 判断任意错误是否为网络不可用。 */
export function isNetworkError(e: unknown): boolean {
  return !!(e as ApiError)?.networkUnavailable
}

/**
 * 从 axios 错误中判定是否属于「网络不可达」。
 *
 * axios 在网络层失败时没有 response（ECONNABORTED / ERR_NETWORK / 超时），
 * 跨域被浏览器拦截时同样没有 response。
 */
export function looksLikeNetworkFailure(error: {
  response?: { status?: number }
  code?: string
  message?: string
  request?: unknown
}): boolean {
  // 有响应 = 请求到达了服务器 = 网络是通的（可能是业务错误或 5xx）。
  // 注意：CORS 被拦截时浏览器不会把响应交给 JS，因此这里拿不到 response。
  if (error.response) return false

  // 一个请求对象存在却没有响应，是「发出去了但没拿到结果」的典型形态：
  // 连接被拒、DNS 失败、超时、跨域被拦截都属于这类。
  if (error.request) return true

  const code = error.code || ''
  const msg = (error.message || '').toLowerCase()

  // 已知的网络层错误码（axios / Node / 浏览器各有不同写法）。
  const networkCodes = [
    'ECONNABORTED',
    'ETIMEDOUT',
    'ECONNREFUSED',
    'ECONNRESET',
    'ENOTFOUND',
    'EAI_AGAIN',
    'ERR_NETWORK',
    'ERR_CANCELED',
    'ERR_CONNECTION_REFUSED',
    'ERR_CONNECTION_TIMED_OUT',
    'ERR_INTERNET_DISCONNECTED',
    'ERR_NAME_NOT_RESOLVED'
  ]
  if (networkCodes.includes(code)) return true

  const patterns = [
    'timeout',
    'network error',
    'networkerror',
    'failed to fetch',
    'fetch failed',
    'load failed',
    'connection refused',
    'connection reset',
    'err_connection',
    'err_internet_disconnected',
    'err_name_not_resolved',
    'getaddrinfo',
    'socket hang up',
    'ns_error_'
  ]
  if (patterns.some((p) => msg.includes(p))) return true

  // 兜底：既没有 response 也没有 request 信息，且不像业务错误 ——
  // 视为网络不可达（宁可提示"更换网络"，也不要误导成"密码错误"）。
  return !msg || msg === 'error' || msg === 'request failed'
}
