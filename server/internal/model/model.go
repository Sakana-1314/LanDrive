// Package model 定义领域对象与枚举常量。
//
// 数据库列、JSON 字段、磁盘路径三者的一一对应关系见 docs/design.md。
package model

import "time"

// 角色。
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// 文件状态。
const (
	StatusActive  = "active"
	StatusTrashed = "trashed"
)

// 上传会话状态。
const (
	UploadUploading = "uploading"
	UploadDone      = "done"
	UploadAborted   = "aborted"
)

// 审计动作。
const (
	ActLogin          = "login"
	ActLoginFailed    = "login_failed"
	ActUpload         = "upload"
	ActRename         = "rename"
	ActDelete         = "delete"
	ActRestore        = "restore"
	ActPurge          = "purge"
	ActDownload       = "download"
	ActUserCreate     = "user_create"
	ActUserUpdate     = "user_update"
	ActUserDelete     = "user_delete"
	ActPasswordChange = "password_change"
	ActPasswordReset  = "password_reset"
	ActSettingsUpdate = "settings_update"
	ActMaintainExpire = "maintain_expire"
	ActMaintainPurge  = "maintain_purge"
	ActMaintainOrphan = "maintain_orphan"
	ActStorageScan    = "storage_scan"
)

// User 账号。dir_rel 是该用户目录相对数据根目录的路径（如 users/12）。
type User struct {
	ID          int64      `json:"id"`
	EmployeeNo  string     `json:"employee_no"`
	Name        string     `json:"name"`
	Password    string     `json:"-"` // bcrypt 哈希，绝不外发
	Role        string     `json:"role"`
	Enabled     bool       `json:"enabled"`
	DirRel      string     `json:"dir_rel"`
	FileCount   int64      `json:"file_count"`
	UsedBytes   int64      `json:"used_bytes"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IsAdmin 报告账号是否为管理员。
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// Public 返回可安全外发给前端的副本（清空密码哈希）。
//
// 必须返回副本而不是原地清空：UserCache 缓存的是指针，
// 若在登录后直接改写 u.Password，会污染缓存，
// 导致后续请求的令牌指纹校验失败（表现为"密码已变更，请重新登录"）。
func (u *User) Public() *User {
	if u == nil {
		return nil
	}
	out := *u
	out.Password = ""
	out.LastLoginAt = nil
	return &out
}

// File 一条文件记录，严格对应磁盘上一个文件。
type File struct {
	ID          int64      `json:"id"`
	OwnerID     int64      `json:"owner_id"`
	OriginalNam string     `json:"original_name"`
	Ext         string     `json:"ext"`
	SizeBytes   int64      `json:"size_bytes"`
	Mime        string     `json:"mime"`
	SHA256      string     `json:"sha256"`
	RelPath     string     `json:"rel_path"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expires_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
	PurgeAt     *time.Time `json:"purge_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// 以下字段来自 users 联查，便于前端直接展示，不落 files 表。
	OwnerName       string `json:"owner_name"`
	OwnerEmployeeNo string `json:"owner_employee_no"`

	// DaysLeft 距到期标记删除的剩余天数（负数表示已过期）。由 handler 计算。
	DaysLeft int `json:"days_left"`
	// IsMine / CanEdit 由 handler 按当前登录者计算。
	IsMine  bool `json:"is_mine"`
	CanEdit bool `json:"can_edit"`
}

// UploadSession 分片上传会话。
type UploadSession struct {
	ID            string    `json:"upload_id"`
	OwnerID       int64     `json:"-"`
	OriginalName  string    `json:"original_name"`
	Ext           string    `json:"ext"`
	SizeBytes     int64     `json:"size_bytes"`
	ChunkSize     int       `json:"chunk_size"`
	TotalChunks   int       `json:"total_chunks"`
	ReceivedBytes int64     `json:"received_bytes"`
	Status        string    `json:"status"`
	DirRel        string    `json:"-"`
	SHA256        string    `json:"-"`
	FileID        *int64    `json:"file_id"` // 合并成功后写入，用于幂等返回
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Uploaded 是已落盘的分片序号（升序）。由 handler 填充。
	Uploaded []int `json:"uploaded"`
}

// Chunk 一个已上传的分片。
type Chunk struct {
	Idx       int    `json:"idx"`
	SizeBytes int64  `json:"size_bytes"`
	RelPath   string `json:"rel_path"`
}

// LogEntry 一条审计日志。
type LogEntry struct {
	ID         int64     `json:"id"`
	UserID     *int64    `json:"user_id"`
	EmployeeNo string    `json:"employee_no"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

// Stats 管理端统计。
type Stats struct {
	Users             int64 `json:"users"`
	Files             int64 `json:"files"`
	Trashed           int64 `json:"trashed"`
	TotalBytes        int64 `json:"total_bytes"`
	TrashedBytes      int64 `json:"trashed_bytes"`
	Expiring7d        int64 `json:"expiring_7d"`
	UploadsInProgress int64 `json:"uploads_in_progress"`
	ExpiredNotPurged  int64 `json:"expired_not_purged"`
}

// OwnerAggregate 按用户目录聚合的文件统计。
type OwnerAggregate struct {
	UserID     int64  `json:"user_id"`
	EmployeeNo string `json:"employee_no"`
	Name       string `json:"name"`
	DirRel     string `json:"dir_rel"`
	FileCount  int64  `json:"file_count"`
	UsedBytes  int64  `json:"used_bytes"`
}
