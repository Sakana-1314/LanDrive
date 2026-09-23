// API 基址解析：单独成模块，便于在 Node 下直接做单元测试
// （web/src/api/index.ts 顶层引用了 vite define 的编译期常量，无法在 Node 里 import）。
//
// 解析优先级（从高到低）：
//   1. 运行时 /config.js 的 apiBaseUrl —— 容器启动时注入，同一个镜像可部署到任意环境；
//      设为 "/api" 即表示「走网页端自身域名的 /api」，由前端镜像内的 nginx 反代到后端容器。
//   2. 构建期 HOST —— 后端域名（仅 origin，不含 /api），拼成 `${HOST}/api`。
//   3. 同源 /api —— 兜底。

/** 运行时配置（由 web/docker-entrypoint.d 脚本生成 /config.js 注入）。 */
export interface RuntimeConfig {
  apiBaseUrl?: string
  probeTimeout?: number
}

/** 去掉结尾斜杠，避免拼出 `//api` 这类地址。 */
function trimTrailingSlashes(v: string): string {
  return v.replace(/\/+$/, '')
}

/**
 * 计算 API 基址。
 *
 * @param runtimeApiBaseUrl /config.js 里的 apiBaseUrl（可为空）
 * @param buildHost 构建期注入的后端域名（仅 origin，可为空）
 */
export function resolveApiBaseUrl(runtimeApiBaseUrl: string | undefined, buildHost: string): string {
  const runtime = trimTrailingSlashes((runtimeApiBaseUrl ?? '').trim())
  // 运行时配置优先级最高：即使构建期注入了后端域名，也允许部署时改走同源反代。
  if (runtime) return runtime

  const host = trimTrailingSlashes((buildHost ?? '').trim())
  if (host) return `${host}/api`

  return '/api'
}

/** 是否为跨域（绝对地址）形态。同源 `/api` 返回 false。 */
export function isCrossOriginBaseUrl(baseUrl: string): boolean {
  return /^https?:\/\//i.test(baseUrl)
}

/** 解析探测超时（毫秒），非法值回退到默认值。 */
export function resolveProbeTimeout(
  runtimeProbeTimeout: number | undefined,
  buildProbeTimeout: unknown,
  fallback = 6000
): number {
  for (const candidate of [runtimeProbeTimeout, buildProbeTimeout]) {
    const n = Number(candidate)
    if (Number.isFinite(n) && n > 0) return n
  }
  return fallback
}
