// Package shares 实现分享链接：创建、撤销、列举，以及免登录的访问解析。
//
// 核心语义（需求明确要求，也是本包的设计中心）：
//
//   - 分享指向的是**文件/目录记录**，不是磁盘路径字符串。因此文件改名、
//     移动目录之后链接依然可用 —— 路径由记录在访问时解析。
//   - 文件被删除（进回收站）时记录仍在，解析结果明确是"目标已被删除"，
//     而不是笼统的"链接失效"，用户能看懂发生了什么。
//   - 记录被彻底清除时靠外键 CASCADE 把分享一并删掉，链接变成"不存在"。
//   - 管理员恢复文件后，同一条链接会自动重新可用（因为它一直指向同一记录）。
package shares

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// ErrForbidden 表示无权操作该分享。
var ErrForbidden = errors.New("无权操作该分享")

// ErrInvalidTarget 表示分享目标不合法（不存在、不属于自己等）。
var ErrInvalidTarget = errors.New("分享目标不合法")

// ErrBadExpire 表示有效期取值不在允许范围内。
var ErrBadExpire = errors.New("有效期只能是 1、3、7、30 天或永久")

// Service 分享业务服务。
type Service struct {
	store *store.Store
	st    *storage.Storage
	set   *settings.Service
}

// New 构造服务。
func New(s *store.Store, st *storage.Storage, set *settings.Service) *Service {
	return &Service{store: s, st: st, set: set}
}

// newToken 生成 32 位十六进制随机凭证。
//
// 必须用 crypto/rand：token 是免登录接口的**唯一凭证**，
// 可预测的 token 等于把所有人的文件公开。
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成分享凭证失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateInput 创建分享的入参。
type CreateInput struct {
	Actor      *model.User
	TargetType string // file / folder
	TargetID   int64
	// ExpireDays 为 nil 表示永久（默认）。其余只允许 model.ShareExpireOptions。
	ExpireDays *int
}

// Create 创建一条分享链接。
//
// 权限：普通用户只能分享**自己拥有的**目标；管理员可以分享任意目标
// （与"管理员可管理任意文件"的既有约定一致）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*model.Share, error) {
	if in.Actor == nil {
		return nil, ErrForbidden
	}
	if in.ExpireDays != nil {
		if !validExpire(*in.ExpireDays) {
			return nil, ErrBadExpire
		}
	}

	sh := &model.Share{
		OwnerID:    in.Actor.ID,
		TargetType: in.TargetType,
		ExpireDays: in.ExpireDays,
	}
	if in.ExpireDays != nil {
		at := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, *in.ExpireDays)
		sh.ExpiresAt = &at
	}

	switch in.TargetType {
	case model.ShareTargetFile:
		f, err := s.store.GetFileForShare(ctx, in.TargetID)
		if err != nil {
			return nil, fmt.Errorf("%w: 文件不存在", ErrInvalidTarget)
		}
		// 已被删除的文件不应再被分享（否则等于把回收站里的东西公开出去）。
		if f.Status != model.StatusActive {
			return nil, fmt.Errorf("%w: 该文件已被删除，无法分享", ErrInvalidTarget)
		}
		if !canShare(in.Actor, f.OwnerID) {
			return nil, fmt.Errorf("%w: 只能分享自己上传的文件", ErrForbidden)
		}
		id := f.ID
		sh.FileID = &id

	case model.ShareTargetFolder:
		fd, err := s.store.GetFolder(ctx, in.TargetID)
		if err != nil {
			return nil, fmt.Errorf("%w: 文件夹不存在", ErrInvalidTarget)
		}
		if !canShare(in.Actor, fd.OwnerID) {
			return nil, fmt.Errorf("%w: 只能分享自己的文件夹", ErrForbidden)
		}
		id := fd.ID
		sh.FolderID = &id

	default:
		return nil, fmt.Errorf("%w: 分享目标类型只能是 file 或 folder", ErrInvalidTarget)
	}

	// token 碰撞概率极低，但仍重试几次而不是直接失败。
	for attempt := 0; attempt < 3; attempt++ {
		token, err := newToken()
		if err != nil {
			return nil, err
		}
		sh.Token = token
		if err := s.store.CreateShare(ctx, sh); err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue // token 撞了，换一个
			}
			return nil, err
		}
		return s.decorate(ctx, sh), nil
	}
	return nil, errors.New("生成分享链接失败，请重试")
}

// canShare 判定 actor 能否分享 ownerID 的资源。
func canShare(actor *model.User, ownerID int64) bool {
	return actor.IsAdmin() || actor.ID == ownerID
}

// validExpire 校验有效期取值。
func validExpire(days int) bool {
	for _, d := range model.ShareExpireOptions {
		if d == days {
			return true
		}
	}
	return false
}

// List 列出分享。
//
// 需求是"所有人都能看到所有人创建的分享链接"，因此 viewer 非管理员时
// 也返回**全部**分享（含创建者信息）。OwnerID 过滤只用于"只看我创建的"。
func (s *Service) List(ctx context.Context, viewer *model.User, onlyMine bool, page, pageSize int) ([]model.Share, int64, error) {
	q := store.ShareListQuery{Page: page, PageSize: pageSize}
	if onlyMine && viewer != nil {
		q.OwnerID = viewer.ID
	}
	items, total, err := s.store.ListShares(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		s.decorate(ctx, &items[i])
	}
	return items, total, nil
}

// Revoke 撤销分享。创建者本人或管理员可撤销。
func (s *Service) Revoke(ctx context.Context, id int64, actor *model.User) error {
	if actor == nil {
		return ErrForbidden
	}
	sh, err := s.store.GetShareByID(ctx, id)
	if err != nil {
		return err
	}
	if !canShare(actor, sh.OwnerID) {
		return fmt.Errorf("%w: 只能撤销自己创建的分享", ErrForbidden)
	}
	n, err := s.store.DeleteShare(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ResolveStatus 免登录访问的解析结果状态。
type ResolveStatus string

const (
	// ResolveOK 链接可用。
	ResolveOK ResolveStatus = "ok"
	// ResolveExpired 链接已过期。
	ResolveExpired ResolveStatus = "expired"
	// ResolveDeleted 目标已被删除（区别于"链接失效"，要明确告知用户）。
	ResolveDeleted ResolveStatus = "deleted"
	// ResolveNotFound token 不存在或已被撤销。
	ResolveNotFound ResolveStatus = "notfound"
)

// Resolved 一次免登录解析的结果。
type Resolved struct {
	Status ResolveStatus
	Share  *model.Share
	// Name/SizeBytes/Mime/Kind 仅当 Status 为 ResolveOK 时有意义。
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Mime      string `json:"mime"`
	Ext       string `json:"ext"`
	// Kind 预览方式（image/pdf/video/text/office/none）。
	Kind string `json:"kind"`
	// FileCount/TotalBytes 目录分享时有意义。
	FileCount  int64 `json:"file_count"`
	TotalBytes int64 `json:"total_bytes"`
	// OwnerName 分享者姓名，页面显示"由谁分享"。
	OwnerName string `json:"owner_name"`
}

// Resolve 解析一个分享 token（**免登录**，不传 actor）。
//
// 刻意不接收任何 id/路径参数：免登录接口只认 token，
// 避免有人拿 token 之外的参数去枚举其他人的文件。
func (s *Service) Resolve(ctx context.Context, token string) (*Resolved, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return &Resolved{Status: ResolveNotFound}, nil
	}
	sh, err := s.store.GetShareByToken(ctx, token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &Resolved{Status: ResolveNotFound}, nil
		}
		return nil, err
	}

	out := &Resolved{Share: sh, OwnerName: sh.OwnerName}

	// 先判过期：过期后即使目标还在也不该继续提供内容。
	if sh.ExpiresAt != nil && !sh.ExpiresAt.After(time.Now().UTC()) {
		out.Status = ResolveExpired
		return out, nil
	}

	switch sh.TargetType {
	case model.ShareTargetFile:
		if sh.FileID == nil {
			return &Resolved{Status: ResolveNotFound}, nil
		}
		// 用 GetFileForShare 而不是 files.Get：后者对 trashed 直接返回
		// ErrNotFound，那样就无法区分"文件被删除"与"链接失效"了。
		f, err := s.store.GetFileForShare(ctx, *sh.FileID)
		if err != nil {
			// 记录已被彻底清除 → 外键本应级联删掉分享；
			// 这里兜底处理历史脏数据。
			return &Resolved{Status: ResolveNotFound}, nil
		}
		if f.Status != model.StatusActive {
			out.Status = ResolveDeleted
			out.Name = f.OriginalNam
			return out, nil
		}
		out.Status = ResolveOK
		out.Name = f.OriginalNam
		out.SizeBytes = f.SizeBytes
		out.Mime = f.Mime
		out.Ext = f.Ext
		// 与登录态预览共用同一套体积规则：免登录链接不该成为绕过预览上限的后门。
		// 分享页拿不到超限文件的内联内容，但仍可下载（下载接口不设这道闸）。
		out.Kind = storage.PreviewKindFor(f.Ext, f.SizeBytes, s.set.Get().PreviewMaxSizeBytes())

	case model.ShareTargetFolder:
		if sh.FolderID == nil {
			return &Resolved{Status: ResolveNotFound}, nil
		}
		fd, err := s.store.GetFolder(ctx, *sh.FolderID)
		if err != nil {
			return &Resolved{Status: ResolveNotFound}, nil
		}
		// 目录是软删除：记录仍在但 status=deleted。
		// 用状态判断而不是"目录里没文件" —— 后者会把**本来就是空目录**的
		// 分享误报成"已被删除"（实测踩过）。
		if fd.Status == model.FolderDeleted {
			out.Status = ResolveDeleted
			out.Name = fd.Name
			return out, nil
		}
		dirRel, err := storage.FolderDirRel(storage.UserDirRel(fd.OwnerEmployeeNo), fd.Path)
		if err != nil {
			return &Resolved{Status: ResolveNotFound}, nil
		}
		cnt, bytes, err := s.store.CountFilesInFolder(ctx, fd.OwnerID, dirRel)
		if err != nil {
			return nil, err
		}
		out.Status = ResolveOK
		out.Name = fd.Name
		out.FileCount = cnt
		out.TotalBytes = bytes

	default:
		return &Resolved{Status: ResolveNotFound}, nil
	}

	// 只有真正可用时才计一次访问。
	if out.Status == ResolveOK {
		if err := s.store.IncrementShareView(ctx, sh.ID); err != nil {
			// 计数失败不影响访问本身。
			_ = err
		}
	}
	return out, nil
}

// FileForDownload 免登录下载：解析 token 后返回文件记录与绝对路径。
//
// 与 Resolve 分开是为了让"下载"这一步能拿到真实路径，
// 同时把权限/状态判断保持在同一个地方（Resolve）。
func (s *Service) FileForDownload(ctx context.Context, token string) (*model.File, string, error) {
	res, err := s.Resolve(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if res.Status != ResolveOK || res.Share.TargetType != model.ShareTargetFile {
		return nil, "", store.ErrNotFound
	}
	f, err := s.store.GetFileForShare(ctx, *res.Share.FileID)
	if err != nil {
		return nil, "", err
	}
	abs, err := s.st.Abs(f.RelPath)
	if err != nil {
		return nil, "", err
	}
	return f, abs, nil
}

// FolderFilesForDownload 目录分享打包下载：返回目录内全部文件及其绝对路径。
// relInArchive 是文件在压缩包内的相对路径（保留目录结构）。
func (s *Service) FolderFilesForDownload(ctx context.Context, token string) (*model.Folder, []ArchiveEntry, error) {
	res, err := s.Resolve(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if res.Status != ResolveOK || res.Share.TargetType != model.ShareTargetFolder {
		return nil, nil, store.ErrNotFound
	}
	fd, err := s.store.GetFolder(ctx, *res.Share.FolderID)
	if err != nil {
		return nil, nil, err
	}
	dirRel, err := storage.FolderDirRel(storage.UserDirRel(fd.OwnerEmployeeNo), fd.Path)
	if err != nil {
		return nil, nil, err
	}
	list, err := s.store.ListFilesInFolder(ctx, fd.OwnerID, dirRel)
	if err != nil {
		return nil, nil, err
	}
	entries := make([]ArchiveEntry, 0, len(list))
	for _, f := range list {
		abs, err := s.st.Abs(f.RelPath)
		if err != nil {
			continue
		}
		// 压缩包内用原始文件名；同目录重名由 rel_path 的 id 部分保证不冲突，
		// 这里简单用原始名，重名时前端看到的是"同名不同 id"的两个条目。
		entries = append(entries, ArchiveEntry{
			AbsPath: abs,
			// 去掉磁盘目录前缀，只保留相对部分（含可能的子目录结构）。
			Name:      strings.TrimPrefix(f.RelPath, dirRel+"/"),
			SizeBytes: f.SizeBytes,
			ModTime:   f.UpdatedAt,
		})
	}
	return fd, entries, nil
}

// ArchiveEntry 压缩包内的一个条目。
type ArchiveEntry struct {
	AbsPath   string
	Name      string
	SizeBytes int64
	ModTime   time.Time
}

// ListExpiredTokens 供维护任务使用：清理过期很久的分享。
func (s *Service) PurgeExpired(ctx context.Context, graceDays int) (int64, error) {
	before := time.Now().UTC().AddDate(0, 0, -graceDays)
	return s.store.PurgeExpiredShares(ctx, before)
}

// decorate 补全列表展示需要的字段：目标名、目标是否已删除、是否已过期。
func (s *Service) decorate(ctx context.Context, sh *model.Share) *model.Share {
	now := time.Now().UTC()
	sh.Expired = sh.ExpiresAt != nil && !sh.ExpiresAt.After(now)
	switch sh.TargetType {
	case model.ShareTargetFile:
		if sh.FileID != nil {
			if f, err := s.store.GetFileForShare(ctx, *sh.FileID); err == nil {
				sh.TargetName = f.OriginalNam
				sh.TargetSizeBytes = f.SizeBytes
				sh.TargetDeleted = f.Status != model.StatusActive
			} else {
				sh.TargetDeleted = true
			}
		}
	case model.ShareTargetFolder:
		if sh.FolderID != nil {
			if fd, err := s.store.GetFolder(ctx, *sh.FolderID); err == nil {
				sh.TargetName = fd.Name
				sh.TargetDeleted = fd.Status == model.FolderDeleted
				if !sh.TargetDeleted {
					if dirRel, err := storage.FolderDirRel(storage.UserDirRel(fd.OwnerEmployeeNo), fd.Path); err == nil {
						if _, bytes, err := s.store.CountFilesInFolder(ctx, fd.OwnerID, dirRel); err == nil {
							sh.TargetSizeBytes = bytes
						}
					}
				}
			} else {
				sh.TargetDeleted = true
			}
		}
	}
	return sh
}
