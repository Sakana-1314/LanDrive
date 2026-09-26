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
	ID      int64 `json:"id"`
	OwnerID int64 `json:"owner_id"`
	// FolderID 所属文件夹；0 表示用户根目录（故意用 0 而不是 NULL，
	// 详见 0003 迁移里的说明）。
	FolderID    int64  `json:"folder_id"`
	OriginalNam string `json:"original_name"`
	Ext         string `json:"ext"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
	SHA256      string `json:"sha256"`
	RelPath     string `json:"rel_path"`
	Status      string `json:"status"`
	// ExpiresAt 到期标记删除的时间点。**nil 表示永久**：该文件不参与到期扫描，
	// 只能由删除 / 管理员彻底删除终结。与 shares.ExpiresAt 是同一套语义。
	//
	// 用 nil 而不是零值 time.Time 或哨兵时间：零值会被 `expires_at <= now`
	// 判成"早就过期"，哨兵时间则要求每处比较都记得排除它 —— 两者都是
	// "某处忘了处理就静默出错"的形状。指针 + 显式 nil 判断不会忘。
	ExpiresAt *time.Time `json:"expires_at"`
	DeletedAt *time.Time `json:"deleted_at"`
	PurgeAt   *time.Time `json:"purge_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`

	// 以下字段来自 users 联查，便于前端直接展示，不落 files 表。
	OwnerName       string `json:"owner_name"`
	OwnerEmployeeNo string `json:"owner_employee_no"`

	// DaysLeft 距到期标记删除的剩余天数（负数表示已过期）。由 handler 计算。
	// Permanent 为 true 时无意义（前端看 Permanent 决定显示"永久"）。
	DaysLeft int `json:"days_left"`
	// Permanent 表示该文件已被设为永久（expires_at IS NULL）。
	// 前端据此显示「永久」角标，而不是把 days_left=0 显示成"今天到期"。
	Permanent bool `json:"permanent"`
	// IsMine / CanEdit 由 handler 按当前登录者计算。
	IsMine  bool `json:"is_mine"`
	CanEdit bool `json:"can_edit"`
}

// UploadSession 分片上传会话。
type UploadSession struct {
	ID            string `json:"upload_id"`
	OwnerID       int64  `json:"-"`
	OriginalName  string `json:"original_name"`
	Ext           string `json:"ext"`
	SizeBytes     int64  `json:"size_bytes"`
	ChunkSize     int    `json:"chunk_size"`
	TotalChunks   int    `json:"total_chunks"`
	ReceivedBytes int64  `json:"received_bytes"`
	Status        string `json:"status"`
	DirRel        string `json:"-"`
	// FolderID 目标文件夹（0=根目录）；complete 时写入文件记录。
	FolderID  int64     `json:"folder_id"`
	SHA256    string    `json:"-"`
	FileID    *int64    `json:"file_id"` // 合并成功后写入，用于幂等返回
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Uploaded 是已落盘的分片序号（升序）。由 handler 填充。
	Uploaded []int `json:"uploaded"`
}

// Chunk 一个已上传的分片。
type Chunk struct {
	Idx       int    `json:"idx"`
	SizeBytes int64  `json:"size_bytes"`
	RelPath   string `json:"rel_path"`
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
	// Pinned 表示"当前登录用户"是否置顶了这个人的目录。
	// 置顶是每人各自一份（user_pins），不是全局标记。
	Pinned bool `json:"pinned"`
}

// 目录状态。删除目录用**软删除**（保留行、path 加墓碑后缀），
// 而不是删行：分享指向目录记录，删行会让"文件夹已被删除"退化成"链接无效"。
const (
	FolderActive  = "active"
	FolderDeleted = "deleted"
)

// Folder 一个文件夹。每人一棵树，path 是相对"用户根目录"的规范化路径。
type Folder struct {
	ID        int64     `json:"id"`
	OwnerID   int64     `json:"owner_id"`
	ParentID  *int64    `json:"parent_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Tombstone 为真表示这是一条已删除目录的记录（path 带墓碑后缀）。
	// 只有分享解析会用它判断"文件夹已被删除"，正常列表一律过滤掉。
	Tombstone bool `json:"-"`

	// 以下由服务层填充，不落表。
	OwnerName       string `json:"owner_name"`
	OwnerEmployeeNo string `json:"owner_employee_no"`
	// FileCount / UsedBytes 是该目录**直接**包含的文件（不含子目录）。
	FileCount int64 `json:"file_count"`
	UsedBytes int64 `json:"used_bytes"`
	// SubFolderCount 直接子目录数。
	SubFolderCount int64 `json:"sub_folder_count"`
}

// Share 一条分享链接。
//
// 指向的是**记录**（FileID / FolderID）而不是磁盘路径：
// 因此文件改名或移动后链接依然可用，而文件被删除后能明确回复"已被删除"，
// 记录被彻底清除时靠外键级联把分享一并删掉。
type Share struct {
	ID         int64  `json:"id"`
	Token      string `json:"token"`
	OwnerID    int64  `json:"owner_id"`
	TargetType string `json:"target_type"` // file / folder
	FileID     *int64 `json:"file_id"`
	FolderID   *int64 `json:"folder_id"`
	// ExpireDays 为 nil 表示永久；expires_at 为 nil 表示永久。
	ExpireDays *int       `json:"expire_days"`
	ExpiresAt  *time.Time `json:"expires_at"`
	ViewCount  int64      `json:"view_count"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// 以下由服务层填充，便于前端列表直接展示。
	OwnerName       string `json:"owner_name"`
	OwnerEmployeeNo string `json:"owner_employee_no"`
	// TargetName 目标名（文件名或文件夹名）。
	TargetName string `json:"target_name"`
	// TargetSizeBytes 仅文件分享有意义；目录分享为 0。
	TargetSizeBytes int64 `json:"target_size_bytes"`
	// TargetDeleted 目标是否已被删除（软删/回收站）。前端据此提示
	// 「分享的文件已被删除」，而不是笼统地说链接失效。
	TargetDeleted bool `json:"target_deleted"`
	// Expired 由服务层按当前时间计算，避免前端各自判断时区。
	Expired bool `json:"expired"`
}

// ShareTargetType 分享目标类型。
const (
	ShareTargetFile   = "file"
	ShareTargetFolder = "folder"
)

// ShareExpireOptions 允许的有效期（天）。nil 表示永久。
// 放在 model 里是因为前后端与校验都要用同一份定义。
var ShareExpireOptions = []int{1, 3, 7, 30}
