// axios 实例与全部接口封装。
//
// 部署形态：前端在**公网**，后端 API 在**内网**，两者不同源。
//   - API 基址来自构建期 HOST（见 .env.example），也可用运行时 /config.js 覆盖；
//   - 网络不可达时统一抛出带 networkUnavailable 标记的错误，
//     由调用方提示「无法在此网络下使用，请更换网络再试！」；
//   - 401 由拦截器统一跳转登录页。
import axios, { type AxiosInstance, type AxiosProgressEvent } from 'axios'
import type {
  FileItem,
  FileQuery,
  Folder,
  FolderListing,
  OrphanReport,
  OwnerAggregate,
  Paged,
  PreviewInfo,
  Settings,
  Share,
  ShareResolved,
  ShareTargetType,
  Stats,
  UploadConfig,
  UploadSession,
  User
} from './types'
import {
  isNetworkError,
  looksLikeNetworkFailure,
  networkError,
  NETWORK_UNAVAILABLE_MESSAGE,
  type ApiError
} from './network'
import {
  isCrossOriginBaseUrl,
  resolveApiBaseUrl,
  resolveProbeTimeout,
  type RuntimeConfig
} from './base-url'
import { isApiReachable } from './health'

export { NETWORK_UNAVAILABLE_MESSAGE, isNetworkError, networkError }
export type { ApiError }

const TOKEN_KEY = 'lanfs-token'

/** 容器运行时注入的配置（由 web/docker-entrypoint.d 脚本生成 /config.js）。 */
declare global {
  interface Window {
    __LANDRIVE_CONFIG__?: RuntimeConfig
  }
}

/** 读取容器启动时注入的运行时配置。 */
function runtimeConfig(): RuntimeConfig {
  return (typeof window !== 'undefined' ? window.__LANDRIVE_CONFIG__ : undefined) ?? {}
}

/**
 * API 基址。解析优先级见 base-url.ts：
 * 运行时 /config.js → 构建期 HOST（__API_HOST__）→ 同源 /api。
 *
 * 前端镜像内的 nginx 可通过 LANDRIVE_API_PROXY 把同源 /api 反代到后端容器，
 * 此时运行时会注入 apiBaseUrl="/api"，浏览器不再跨域直连内网。
 */
export const API_BASE_URL: string = resolveApiBaseUrl(runtimeConfig().apiBaseUrl, __API_HOST__)

/** 连通性探测超时（毫秒）。 */
const PROBE_TIMEOUT = resolveProbeTimeout(runtimeConfig().probeTimeout, __API_PROBE_TIMEOUT__)

/** 是否配置了跨域的内网 API（公网部署形态）。 */
export const IS_CROSS_ORIGIN = isCrossOriginBaseUrl(API_BASE_URL)

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

export const http: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  timeout: 120000
})

http.interceptors.request.use((config) => {
  const token = getToken()
  if (token) {
    config.headers = config.headers || {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

/** onUnauthorized 由路由层注入，避免 api 层依赖 router。 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn
}

/** onNetworkUnavailable 由应用层注入：网络不通时弹全局提示。 */
let onNetworkUnavailable: ((msg: string) => void) | null = null
export function setNetworkUnavailableHandler(fn: (msg: string) => void): void {
  onNetworkUnavailable = fn
}

/** 网络不可用提示的节流，避免并发请求弹出一堆重复提示。 */
let lastNetworkNoticeAt = 0
function notifyNetworkUnavailable(): void {
  const now = Date.now()
  if (now - lastNetworkNoticeAt < 3000) return
  lastNetworkNoticeAt = now
  onNetworkUnavailable?.(NETWORK_UNAVAILABLE_MESSAGE)
}

http.interceptors.response.use(
  (res) => res,
  async (error) => {
    const status = error?.response?.status

    // 请求未到达服务器：网络不可达 / 超时 / 跨域被浏览器拦截。
    if (looksLikeNetworkFailure(error)) {
      notifyNetworkUnavailable()
      return Promise.reject(networkError(error))
    }

    // 二进制请求（responseType: 'blob'）失败时，错误体也是 Blob，
    // 需要先读成文本才能拿到后端返回的中文提示。
    let payload = error?.response?.data
    if (payload instanceof Blob) {
      try {
        const text = await payload.text()
        payload = JSON.parse(text)
      } catch {
        payload = undefined
      }
    }

    const msg: string = payload?.error || error?.message || '请求失败'
    if (status === 401) {
      clearToken()
      onUnauthorized?.()
    }
    const err = new Error(msg) as ApiError
    err.status = status
    err.data = payload
    return Promise.reject(err)
  }
)

/** 从响应头或自定义数据中提取错误消息。 */
export function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}

/**
 * 探测接口是否可达（用于进入应用前判断当前网络）。
 *
 * 返回 true 表示内网接口可达；false 表示当前网络访问不到。
 * 注意：即使返回 401/403 也说明网络可达。
 */
export async function probeHealth(): Promise<boolean> {
  try {
    const res = await axios.get(`${API_BASE_URL}/health`, { timeout: PROBE_TIMEOUT })
    // 同源反代未配置时，nginx 的 SPA 回退会返回 200 + HTML 首页。
    // 仅凭状态码会误判为"已连通"，因此这里以响应体形状为准。
    return isApiReachable({
      status: res.status,
      contentType: res.headers?.['content-type'] as string | undefined,
      data: res.data
    })
  } catch (e) {
    const err = e as {
      response?: { status?: number; data?: unknown; headers?: Record<string, string> }
      code?: string
      message?: string
    }
    // 有响应说明请求到达了服务器，但只有拿到本系统 API 的 JSON 才算连通：
    // 502/504（反代上游不可达）或 404 兜底页都说明接口其实不可用。
    if (err?.response) {
      return isApiReachable({
        status: err.response.status,
        contentType: err.response.headers?.['content-type'],
        data: err.response.data
      })
    }
    return !looksLikeNetworkFailure(err)
  }
}

// --- 认证 ---

export interface LoginResult {
  token: string
  expires_at: string
  user: User
}

export async function login(employeeNo: string, password: string): Promise<LoginResult> {
  const { data } = await http.post<LoginResult>('/auth/login', {
    employee_no: employeeNo,
    password
  })
  return data
}

export async function fetchMe(): Promise<{ user: User; settings: Settings }> {
  const { data } = await http.get<{ user: User; settings: Settings }>('/auth/me')
  return data
}

export async function changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await http.post('/auth/password', { old_password: oldPassword, new_password: newPassword })
}

// --- 文件 ---

export async function listFiles(q: FileQuery): Promise<Paged<FileItem>> {
  const { data } = await http.get<Paged<FileItem>>('/files', { params: q })
  return data
}

export async function listOwners(): Promise<{ items: OwnerAggregate[]; total: number }> {
  const { data } = await http.get<{ items: OwnerAggregate[]; total: number }>('/files/owners')
  return data
}

/** 置顶某人的人员目录（对当前账号生效）。 */
export async function pinOwner(id: number): Promise<void> {
  await http.put(`/files/owners/${id}/pin`)
}

/** 取消置顶。 */
export async function unpinOwner(id: number): Promise<void> {
  await http.delete(`/files/owners/${id}/pin`)
}

// --- 文件夹 ---

/** 列出某一层目录（folderId 为 0 表示根层）。 */
export async function listFolders(q: { owner_id?: number; folder_id?: number }): Promise<FolderListing> {
  const { data } = await http.get<FolderListing>('/folders', { params: q })
  return data
}

export async function createFolder(payload: {
  name: string
  parent_id?: number
  owner_id?: number
}): Promise<Folder> {
  const { data } = await http.post<Folder>('/folders', payload)
  return data
}

export async function renameFolder(
  id: number,
  payload: { name: string; parent_id?: number }
): Promise<Folder> {
  const { data } = await http.patch<Folder>(`/folders/${id}`, payload)
  return data
}

export async function deleteFolder(id: number): Promise<{ soft_deleted_files: number }> {
  const { data } = await http.delete<{ soft_deleted_files: number }>(`/folders/${id}`)
  return data
}

// --- 分享 ---

/** 可选的有效期（天）；由服务端给出，避免前后端各硬编码一套。 */
export async function shareExpireOptions(): Promise<{ expire_days: number[] }> {
  const { data } = await http.get<{ expire_days: number[] }>('/shares/options')
  return data
}

/**
 * 创建分享。
 * expireDays 传 null/undefined 表示**永久**（默认），其余只允许 1/3/7/30。
 */
export async function createShare(payload: {
  target_type: ShareTargetType
  target_id: number
  expire_days?: number | null
}): Promise<Share> {
  const { data } = await http.post<Share>('/shares', payload)
  return data
}

/** 列出分享。mine=true 只看自己创建的；默认返回所有人的（需求要求人人可见）。 */
export async function listShares(q: { mine?: boolean; page?: number; page_size?: number }): Promise<Paged<Share>> {
  const { data } = await http.get<Paged<Share>>('/shares', {
    params: { mine: q.mine ? 1 : undefined, page: q.page, page_size: q.page_size }
  })
  return data
}

export async function revokeShare(id: number): Promise<void> {
  await http.delete(`/shares/${id}`)
}

/**
 * 免登录解析分享。**不需要令牌** —— 但本函数仍走同一个 axios 实例，
 * 因为请求头里带不带 token 与服务端是否放行无关（该路由在鉴权组之外）。
 */
export async function resolveShare(token: string): Promise<ShareResolved> {
  const { data } = await http.get<ShareResolved>(`/s/${encodeURIComponent(token)}`)
  return data
}

/** 免登录内容地址（用于 img/iframe/video 的 src，不能走 axios）。 */
export function shareContentUrl(token: string): string {
  return `${API_BASE_URL}/s/${encodeURIComponent(token)}/content`
}

/** 免登录下载地址。 */
export function shareDownloadUrl(token: string): string {
  return `${API_BASE_URL}/s/${encodeURIComponent(token)}/download`
}

export async function getFile(id: number): Promise<FileItem> {
  const { data } = await http.get<FileItem>(`/files/${id}`)
  return data
}

export async function getPreviewInfo(id: number): Promise<PreviewInfo> {
  const { data } = await http.get<PreviewInfo>(`/files/${id}/preview`)
  return data
}

export async function renameFile(id: number, name: string): Promise<FileItem> {
  const { data } = await http.patch<FileItem>(`/files/${id}`, { original_name: name })
  return data
}

export async function deleteFile(id: number): Promise<void> {
  await http.delete(`/files/${id}`)
}

/** 取文件内容为 Blob（预览用）。 */
export async function fetchFileBlob(
  id: number,
  onProgress?: (e: AxiosProgressEvent) => void
): Promise<Blob> {
  const { data } = await http.get<Blob>(`/files/${id}/content`, {
    responseType: 'blob',
    onDownloadProgress: onProgress
  })
  return data
}

/** 触发浏览器下载（blob 方式，保证原始文件名正确）。 */
export async function downloadFile(id: number, name: string): Promise<void> {
  const blob = await fetchFileBlob(id)
  saveBlob(blob, name)
}

export function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 延迟释放，避免部分浏览器下载被中断。
  setTimeout(() => URL.revokeObjectURL(url), 10000)
}

// --- 分片上传 ---

export async function uploadConfig(): Promise<UploadConfig> {
  const { data } = await http.get<UploadConfig>('/uploads/config')
  return data
}

export async function initUpload(
  fileName: string,
  fileSize: number,
  sha256?: string
): Promise<UploadSession> {
  const { data } = await http.post<UploadSession>('/uploads/init', {
    file_name: fileName,
    file_size: fileSize,
    sha256: sha256 || ''
  })
  return data
}

export async function getUpload(id: string): Promise<UploadSession> {
  const { data } = await http.get<UploadSession>(`/uploads/${id}`)
  return data
}

export async function putChunk(
  id: string,
  idx: number,
  blob: Blob,
  onProgress?: (e: AxiosProgressEvent) => void
): Promise<{ received_bytes: number; uploaded: number[] }> {
  const { data } = await http.put<{ received_bytes: number; uploaded: number[] }>(
    `/uploads/${id}/chunks/${idx}`,
    blob,
    {
      headers: { 'Content-Type': 'application/octet-stream' },
      timeout: 0,
      onUploadProgress: onProgress
    }
  )
  return data
}

export async function completeUpload(
  id: string
): Promise<{ file: FileItem; created: boolean; sha256: string }> {
  const { data } = await http.post<{ file: FileItem; created: boolean; sha256: string }>(
    `/uploads/${id}/complete`,
    {},
    { timeout: 0 }
  )
  return data
}

export async function cancelUpload(id: string): Promise<void> {
  await http.delete(`/uploads/${id}`)
}

// --- 管理端 ---

export async function adminListUsers(q: {
  q?: string
  page?: number
  page_size?: number
}): Promise<Paged<User>> {
  const { data } = await http.get<Paged<User>>('/admin/users', { params: q })
  return data
}

export async function adminCreateUser(payload: {
  employee_no: string
  name: string
  role: string
  password: string
}): Promise<User> {
  const { data } = await http.post<User>('/admin/users', payload)
  return data
}

export async function adminUpdateUser(
  id: number,
  payload: { name?: string; role?: string; enabled?: boolean }
): Promise<User> {
  const { data } = await http.patch<User>(`/admin/users/${id}`, payload)
  return data
}

export async function adminResetPassword(id: number, newPassword: string): Promise<void> {
  await http.post(`/admin/users/${id}/password`, { new_password: newPassword })
}

/**
 * 删除账号。
 *
 * 服务端语义：先把该账号名下文件软删除（进回收站）视为已清理，
 * 再删除账号，并清掉其磁盘目录 —— 目录按工号命名，残留内容会被
 * 将来同工号的新账号"继承"。因此这里不接受"是否彻底删除"的选项。
 */
export async function adminDeleteUser(id: number): Promise<void> {
  await http.delete(`/admin/users/${id}`)
}

export async function adminSettings(): Promise<Settings> {
  const { data } = await http.get<Settings>('/admin/settings')
  return data
}

export async function adminUpdateSettings(patch: Partial<Settings>): Promise<Settings> {
  const { data } = await http.put<Settings>('/admin/settings', patch)
  return data
}

export async function adminListFiles(q: FileQuery & { status?: string }): Promise<Paged<FileItem>> {
  const { data } = await http.get<Paged<FileItem>>('/admin/files', { params: q })
  return data
}

export async function adminRestoreFile(id: number): Promise<FileItem> {
  const { data } = await http.post<FileItem>(`/admin/files/${id}/restore`)
  return data
}

export async function adminPurgeFile(id: number): Promise<void> {
  await http.delete(`/admin/files/${id}`)
}

export async function adminStats(): Promise<Stats> {
  const { data } = await http.get<Stats>('/admin/stats')
  return data
}

export async function adminScanStorage(): Promise<OrphanReport> {
  const { data } = await http.post<OrphanReport>('/admin/storage/scan')
  return data
}

/** 当前前端使用的 API 地址与部署形态（用于登录页/设置页排障展示）。 */
export function apiEndpointInfo(): { base_url: string; cross_origin: boolean } {
  return { base_url: API_BASE_URL, cross_origin: IS_CROSS_ORIGIN }
}
