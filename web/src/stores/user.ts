// 轻量全局状态（用 reactive 单例，不引入 Pinia）。
import { reactive } from 'vue'
import type { Settings, User } from '@/api/types'
import { fetchMe, getToken, isNetworkError } from '@/api'
import { clearOwners } from '@/stores/owners'

interface State {
  user: User | null
  settings: Settings | null
  ready: boolean
  loading: boolean
  /** 最近一次拉取用户信息失败的原因，供路由守卫区分「网络不通」与「令牌失效」。 */
  lastError: unknown
}

export const state = reactive<State>({
  user: null,
  settings: null,
  ready: false,
  loading: false,
  lastError: null
})

export function isLoggedIn(): boolean {
  return !!state.user
}

export function isAdmin(): boolean {
  return state.user?.role === 'admin'
}

/** 拉取当前用户与配置；未登录或令牌失效时清空状态。 */
export async function loadMe(force = false): Promise<boolean> {
  if (state.loading) return !!state.user
  if (state.ready && !force && state.user) return true
  if (!getToken()) {
    state.ready = true
    return false
  }
  state.loading = true
  try {
    const { user, settings } = await fetchMe()
    state.user = user
    state.settings = settings
    state.lastError = null
    state.ready = true
    return true
  } catch (e) {
    state.user = null
    state.settings = null
    state.lastError = e
    state.ready = true
    return false
  } finally {
    state.loading = false
  }
}

export function setUser(user: User, settings?: Settings): void {
  state.user = user
  if (settings) state.settings = settings
  state.ready = true
}

/** 最近一次失败是否因为当前网络访问不到内网接口。 */
export function lastFailureWasNetwork(): boolean {
  return isNetworkError(state.lastError)
}

export function clearUser(): void {
  state.user = null
  state.settings = null
  state.ready = true
  // 用户目录（含置顶顺序）是按账号存的，换账号必须清掉，
  // 否则会看到上一个账号的置顶顺序。放在这里是为了让所有登出路径都不遗漏。
  clearOwners()
}

/** 上传相关配置的本地缓存（体积上限、允许类型、分片大小）。 */
export const uploadState = reactive({
  maxFileSize: 0,
  chunkSize: 0,
  maxFileSizeMB: 0,
  chunkSizeMB: 0,
  allowedExtensions: [] as string[],
  allowAll: true,
  uploadEnabled: true
})

export function applyUploadConfig(c: {
  max_file_size: number
  chunk_size: number
  max_file_size_mb: number
  chunk_size_mb: number
  allowed_extensions: string[]
  allow_all: boolean
  upload_enabled: boolean
}): void {
  uploadState.maxFileSize = c.max_file_size
  uploadState.chunkSize = c.chunk_size
  uploadState.maxFileSizeMB = c.max_file_size_mb
  uploadState.chunkSizeMB = c.chunk_size_mb
  uploadState.allowedExtensions = c.allowed_extensions
  uploadState.allowAll = c.allow_all
  uploadState.uploadEnabled = c.upload_enabled
}

/** 校验一个待上传文件是否满足当前策略，返回错误消息（null 表示通过）。 */
export function validateFile(file: File): string | null {
  if (!uploadState.uploadEnabled) return '管理员已暂停上传功能'
  if (uploadState.maxFileSize > 0 && file.size > uploadState.maxFileSize) {
    return `文件超过单文件上限 ${uploadState.maxFileSizeMB} MB`
  }
  if (!uploadState.allowAll && uploadState.allowedExtensions.length > 0) {
    const dot = file.name.lastIndexOf('.')
    const ext = dot > 0 ? file.name.slice(dot).toLowerCase() : ''
    if (!ext || !uploadState.allowedExtensions.includes(ext)) {
      return `不允许上传 ${ext || '无扩展名'} 类型的文件`
    }
  }
  return null
}
