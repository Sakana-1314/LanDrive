// 健康探测响应的判定：区分「确实连到本系统 API」与「拿到的是别的东西」。
//
// 为什么需要单独判定：前端镜像内的 nginx 用 /api 同源反代后，/api 与前端页面同域。
// 一旦反代没配置（或指向了错误的后端），请求 /api/health 会被 SPA 回退规则接住，
// 返回 **200 + HTML 首页**。只看 HTTP 状态码会误判为「内网已连通」，
// 用户随后登录会得到一个莫名其妙的错误，而不是「无法在此网络下使用，请更换网络再试！」。
//
// 因此这里以**响应体形状**为准：本系统 /api/health 必定返回含 status / service 的 JSON。

/** 探测响应的关键字段。 */
export interface HealthProbeResponse {
  status?: number
  contentType?: string
  data?: unknown
}

/** 响应体是否为本系统 API 返回的 JSON（而非 HTML 兜底页 / 网关错误页）。 */
export function isApiHealthPayload(data: unknown): boolean {
  if (!data || typeof data !== 'object' || Array.isArray(data)) return false
  const o = data as Record<string, unknown>
  // 后端 /api/health 固定返回 status=ok|degraded 与 service=lan-drive-api。
  return typeof o.status === 'string' || typeof o.service === 'string'
}

/** 是否以 JSON 形式返回（辅助判断，不单独作为依据）。 */
export function looksLikeJson(contentType: string | undefined): boolean {
  return /application\/json/i.test(contentType ?? '')
}

/**
 * 判定探测结果。
 *
 * 只有拿到本系统 API 的 JSON 载荷才算可达；
 * 200 + HTML（SPA 回退）、502/504（反代上游不可达）等一律视为不可达，
 * 以便前端给出「无法在此网络下使用，请更换网络再试！」。
 */
export function isApiReachable(res: HealthProbeResponse | undefined): boolean {
  if (!res || typeof res.status !== 'number') return false
  return isApiHealthPayload(res.data)
}
