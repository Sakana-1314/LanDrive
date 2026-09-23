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
}

export interface LogEntry {
  id: number
  user_id: number | null
  employee_no: string
  action: string
  target_type: string
  target_id: string
  detail: string
  ip: string
  created_at: string
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
