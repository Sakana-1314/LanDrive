// 与后端 docs/design.md 第 5 节一一对应的类型定义。

export type Role = 'admin' | 'user'
export type FileStatus = 'active' | 'trashed'
export type UploadStatus = 'uploading' | 'done' | 'aborted'

export interface User {
  id: number
  employee_no: string
  name: string
  role: Role
  enabled: boolean
  dir_rel: string
  file_count: number
  used_bytes: number
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export interface FileItem {
  id: number
  owner_id: number
  owner_name: string
  owner_employee_no: string
  original_name: string
  ext: string
  size_bytes: number
  mime: string
  sha256: string
  rel_path: string
  status: FileStatus
  expires_at: string
  deleted_at: string | null
  purge_at: string | null
  days_left: number
  is_mine: boolean
  can_edit: boolean
  created_at: string
  updated_at: string
}

export interface UploadSession {
  upload_id: string
  original_name: string
  ext: string
  size_bytes: number
  chunk_size: number
  total_chunks: number
  received_bytes: number
  status: UploadStatus
  uploaded: number[]
  created_at: string
  updated_at: string
}

export interface Settings {
  max_file_size_mb: number
  allowed_extensions: string
  retention_days: number
  trash_days: number
  chunk_size_mb: number
  upload_enabled: boolean
}

export interface UploadConfig {
  max_file_size_mb: number
  max_file_size: number
  chunk_size_mb: number
  chunk_size: number
  allowed_extensions: string[]
  allow_all: boolean
  upload_enabled: boolean
}

export interface Stats {
  users: number
  files: number
  trashed: number
  total_bytes: number
  trashed_bytes: number
  expiring_7d: number
  uploads_in_progress: number
  expired_not_purged: number
}

export interface OwnerAggregate {
  user_id: number
  employee_no: string
  name: string
  dir_rel: string
  file_count: number
  used_bytes: number
  /** 当前登录账号是否置顶了这个人的目录（置顶是每人各一份）。 */
  pinned: boolean
}


export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export type PreviewKind =
  | 'docx'
  | 'xlsx'
  | 'pptx'
  | 'pdf'
  | 'image'
  | 'video'
  | 'audio'
  | 'text'
  | 'legacy-office'
  | 'unsupported'

export interface PreviewInfo {
  id: number
  name: string
  ext: string
  size_bytes: number
  mime: string
  kind: PreviewKind
  content_url: string
  note: string
}

export interface OrphanReport {
  orphans: string[]
  missing: string[]
  invalid: string[]
  scanned_at: string
  total_files_on_disk: number
}

export interface FileQuery {
  scope?: 'all' | 'mine'
  owner_id?: number
  q?: string
  ext?: string
  page?: number
  page_size?: number
  sort?: string
  order?: 'asc' | 'desc'
}

/** 文件夹。每人一棵树，path 相对"用户根目录"（磁盘对应 users/<工号>/<path>）。 */
export interface Folder {
  id: number
  owner_id: number
  parent_id: number | null
  name: string
  path: string
  created_at: string
  updated_at: string
  owner_name: string
  owner_employee_no: string
  /** 该目录**直接**包含的文件数（不含子目录） */
  file_count: number
  used_bytes: number
  sub_folder_count: number
}

/** 一层目录的内容（含面包屑）。 */
export interface FolderListing {
  owner_id: number
  folder_id: number
  folders: Folder[]
  breadcrumb: Folder[]
  current: Folder | null
}

export type ShareTargetType = 'file' | 'folder'

/** 一条分享链接。 */
export interface Share {
  id: number
  token: string
  owner_id: number
  target_type: ShareTargetType
  file_id: number | null
  folder_id: number | null
  /** null 表示永久有效 */
  expire_days: number | null
  expires_at: string | null
  view_count: number
  created_at: string
  updated_at: string
  owner_name: string
  owner_employee_no: string
  target_name: string
  target_size_bytes: number
  /** 目标已被删除（软删/回收站）：界面要提示"分享的文件已被删除" */
  target_deleted: boolean
  expired: boolean
}

/**
 * 免登录访问分享的结果状态。
 *
 * 必须区分 expired / deleted / notfound 三种：文案不同，
 * 用户需要知道是"过期了"还是"文件被删了"还是"链接是错的"。
 */
export type ShareResolveStatus = 'ok' | 'expired' | 'deleted' | 'notfound'

export interface ShareResolved {
  status: ShareResolveStatus
  name: string
  size_bytes: number
  mime: string
  ext: string
  kind: string
  file_count: number
  total_bytes: number
  owner_name: string
  target_type: ShareTargetType | ''
  expire_days: number | null
  expires_at: string | null
  created_at: string | null
  view_count: number
}
