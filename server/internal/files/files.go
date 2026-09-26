// Package files 实现文件的业务规则：查询、权限判定、改名、软删除、恢复与彻底删除。
//
// 所有涉及磁盘的操作都先算好相对路径，且相对路径只来自数据库或本包生成。
package files

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// ErrForbidden 表示当前用户无权操作该文件。
var ErrForbidden = errors.New("无权操作该文件")

// ErrCannotPinSelf 表示试图置顶自己的目录。
var ErrCannotPinSelf = errors.New("不能置顶自己的目录")

// Service 文件业务服务。
type Service struct {
	store *store.Store
	st    *storage.Storage
	set   *settings.Service
}

// New 构造服务。
func New(s *store.Store, st *storage.Storage, set *settings.Service) *Service {
	return &Service{store: s, st: st, set: set}
}

// ListOptions 描述一次文件查询。
type ListOptions struct {
	// Actor 是当前登录者，用于计算 is_mine / can_edit。
	Actor *model.User
	// Scope 为 "all" 或 "mine"。
	Scope string
	// OwnerID 指定只看某个用户目录（0 = 不限）。
	OwnerID int64
	// FolderID 只看某个文件夹内的文件（0 且 FolderRootOnly=false 时不按目录过滤）。
	FolderID int64
	// FolderRootOnly 只看根目录下的文件。需要独立开关是因为
	// "根目录"本身就是 folder_id = 0，没法用零值同时表达"不过滤"。
	FolderRootOnly bool
	// Status 为空时只看 active；管理员可传 "trashed" / "all"。
	Status   string
	Keyword  string
	Ext      string
	Sort     string
	Order    string
	Page     int
	PageSize int
}

// ListResult 是分页查询结果。
type ListResult struct {
	Items    []model.File `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// List 查询文件列表，并填充展示字段。
//
// 权限规则：普通用户永远看不到 trashed 文件（哪怕是自己上传的）；
// 只有管理员可以显式查询回收站。
func (s *Service) List(ctx context.Context, opt ListOptions) (*ListResult, error) {
	page, pageSize := normalizePage(opt.Page, opt.PageSize)

	status := opt.Status
	if opt.Actor == nil || !opt.Actor.IsAdmin() {
		// 普通用户强制只看 active。
		status = model.StatusActive
	}
	if status == "" {
		status = model.StatusActive
	}

	ownerID := opt.OwnerID
	if opt.Scope == "mine" {
		if opt.Actor == nil {
			return nil, ErrForbidden
		}
		ownerID = opt.Actor.ID
	}

	q := store.FileQuery{
		OwnerID:        ownerID,
		FolderID:       opt.FolderID,
		FolderRootOnly: opt.FolderRootOnly,
		Status:         status,
		Keyword:        opt.Keyword,
		Ext:            opt.Ext,
		Sort:           opt.Sort,
		Order:          opt.Order,
		Page:           page,
		PageSize:       pageSize,
	}
	items, total, err := s.store.ListFiles(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = s.decorate(&items[i], opt.Actor)
	}
	return &ListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get 返回单个文件（已填充展示字段）。
func (s *Service) Get(ctx context.Context, id int64, actor *model.User) (*model.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.Status == model.StatusTrashed && (actor == nil || !actor.IsAdmin()) {
		// 回收站文件对普通用户等同于不存在。
		return nil, store.ErrNotFound
	}
	out := s.decorate(f, actor)
	return &out, nil
}

// Decorate 为一个文件记录补全展示字段（days_left / is_mine / can_edit）。
//
// 所有对外返回文件的地方都必须经过它：上传完成接口若直接返回数据库原始行，
// 前端会看到 days_left=0、can_edit=false 这类错误信息。
func (s *Service) Decorate(f *model.File, actor *model.User) model.File {
	return s.decorate(f, actor)
}

// decorate 计算 days_left / permanent / is_mine / can_edit。
func (s *Service) decorate(f *model.File, actor *model.User) model.File {
	out := *f
	out.Permanent = out.ExpiresAt == nil
	if out.Permanent {
		// 永久文件没有"还剩几天"的概念。留 0 而不是算成一个巨大数字：
		// 前端看 Permanent 决定显示「永久」，0 只作为"该字段无意义"的哨兵；
		// 若算成 MaxInt，按到期时间排序时永久文件会冒到最前，与直觉相反。
		out.DaysLeft = 0
	} else {
		now := time.Now().UTC()
		out.DaysLeft = int(math.Ceil(out.ExpiresAt.Sub(now).Hours() / 24))
	}
	if actor != nil {
		out.IsMine = out.OwnerID == actor.ID
		// 管理员可管理任意文件；普通用户只能动自己上传的、且仍在有效期内的文件。
		out.CanEdit = actor.IsAdmin() || (out.IsMine && out.Status == model.StatusActive)
	}
	return out
}

// Rename 重命名文件（只改数据库中的原始文件名，磁盘文件名不变）。
func (s *Service) Rename(ctx context.Context, id int64, newName string, actor *model.User) (*model.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkCanModify(f, actor); err != nil {
		return nil, err
	}

	name := storage.SanitizeName(newName)
	if name == "" {
		return nil, fmt.Errorf("%w: 文件名不能为空", store.ErrInvalidInput)
	}
	// 扩展名不可变：避免用户把 .exe 改成 .txt 绕过类型策略。
	base, _ := storage.SplitName(name)
	if err := s.store.RenameFile(ctx, id, storage.JoinName(base, f.Ext)); err != nil {
		return nil, err
	}
	updated, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.decorate(updated, actor)
	return &out, nil
}

// SoftDelete 把文件移入回收站（软删除）。属主或管理员可操作。
func (s *Service) SoftDelete(ctx context.Context, id int64, actor *model.User) (*model.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkCanModify(f, actor); err != nil {
		return nil, err
	}
	set := s.set.Get()
	now := time.Now().UTC().Truncate(time.Second)
	purgeAt := now.AddDate(0, 0, set.TrashDays)
	if err := s.store.MarkTrashed(ctx, id, now, purgeAt); err != nil {
		return nil, err
	}
	updated, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.decorate(updated, actor)
	return &out, nil
}

// Restore 从回收站恢复（仅管理员）。
func (s *Service) Restore(ctx context.Context, id int64, actor *model.User) (*model.File, error) {
	if actor == nil || !actor.IsAdmin() {
		return nil, ErrForbidden
	}
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.Status != model.StatusTrashed {
		return nil, fmt.Errorf("%w: 该文件不在回收站中", store.ErrState)
	}
	// 原来是永久文件就**保持永久**（expiresAt=nil）：软删只是状态变化，
	// 恢复的语义是"回到删除前的样子"，顺手把它降级成有期限是越权改动用户的设定。
	// 只有原本就有期限的文件才按当前保留天数重算 —— 与"重新开始计时"的既有约定一致。
	var expiresAt *time.Time
	if f.ExpiresAt != nil {
		at := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, s.set.Get().RetentionDays)
		expiresAt = &at
	}
	if err := s.store.RestoreFile(ctx, id, expiresAt); err != nil {
		return nil, err
	}
	updated, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.decorate(updated, actor)
	return &out, nil
}

// Purge 立即彻底删除：先删磁盘文件，再删数据库记录（仅管理员）。
// 磁盘文件已不存在时同样删除记录，保证数据库与磁盘最终一致。
func (s *Service) Purge(ctx context.Context, id int64, actor *model.User) (*model.File, error) {
	if actor == nil || !actor.IsAdmin() {
		return nil, ErrForbidden
	}
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.st.Remove(f.RelPath); err != nil {
		// 删除失败时保留记录，交由后续清理任务重试，避免产生幽灵记录。
		return nil, fmt.Errorf("删除磁盘文件失败: %w", err)
	}
	if err := s.store.DeleteFileRow(ctx, id); err != nil {
		return nil, err
	}
	s.cleanupEmptyDir(f.RelPath)
	out := s.decorate(f, actor)
	return &out, nil
}

// PurgeByOwner 删除某用户的全部文件（删除账号前调用），返回删除条数。
func (s *Service) PurgeByOwner(ctx context.Context, ownerID int64) (int, error) {
	list, err := s.store.ListFilesByOwner(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, f := range list {
		if err := s.st.Remove(f.RelPath); err != nil {
			return deleted, fmt.Errorf("删除磁盘文件 %s 失败: %w", f.RelPath, err)
		}
		if err := s.store.DeleteFileRow(ctx, f.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// checkCanModify 判定 actor 能否修改/删除该文件。
func (s *Service) checkCanModify(f *model.File, actor *model.User) error {
	if actor == nil {
		return ErrForbidden
	}
	if actor.IsAdmin() {
		return nil
	}
	if f.OwnerID != actor.ID {
		return fmt.Errorf("%w: 只能修改或删除自己上传的文件", ErrForbidden)
	}
	if f.Status != model.StatusActive {
		return fmt.Errorf("%w: 文件已被删除，无法操作", store.ErrState)
	}
	return nil
}

// ErrPermanentDisabled 表示管理员关闭了永久功能（配额为 0）。
var ErrPermanentDisabled = errors.New("管理员未开放永久保存")

// ErrPermanentQuota 表示永久空间不足。错误信息里带上具体差额，供前端直接展示。
var ErrPermanentQuota = errors.New("永久空间不足")

// PermanentStatus 是"永久空间当前状况"，前端据此在二次确认里提示用户。
type PermanentStatus struct {
	Enabled    bool  `json:"enabled"`
	QuotaBytes int64 `json:"quota_bytes"`
	UsedBytes  int64 `json:"used_bytes"`
	FreeBytes  int64 `json:"free_bytes"`
}

// PermanentStatusOf 返回全站永久空间的使用情况。
//
// UsedBytes 可能大于 QuotaBytes（管理员把配额调低过）：此时 FreeBytes 记 0，
// 而不是给一个负数 —— 前端拿负数去算进度条会画出诡异的图形。
func (s *Service) PermanentStatusOf(ctx context.Context) (PermanentStatus, error) {
	cfg := s.set.Get()
	used, err := s.store.PermanentUsage(ctx)
	if err != nil {
		return PermanentStatus{}, err
	}
	out := PermanentStatus{
		Enabled:    cfg.PermanentEnabled(),
		QuotaBytes: cfg.PermanentQuotaBytes(),
		UsedBytes:  used,
	}
	if out.QuotaBytes > used {
		out.FreeBytes = out.QuotaBytes - used
	}
	return out, nil
}

// SetPermanent 把单个文件设为永久（permanent=true）或改为有期限。
//
// 权限与改名/删除一致（属主或管理员），且只对 active 文件开放：
// 回收站里的文件先恢复再说 —— 否则用户在回收站里设了永久、却因为文件
// 马上被清理而毫无意义，白等一场。
//
// 改为有期限时按当前 retention_days 重新计算到期时间（与"恢复"同一口径）。
func (s *Service) SetPermanent(ctx context.Context, id int64, permanent bool, actor *model.User) (*model.File, error) {
	f, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkCanModify(f, actor); err != nil {
		return nil, err
	}
	// checkCanModify 对管理员直接放行（管理员本来就要能打理回收站），但"设为永久"
	// 对回收站里的文件没有意义：它不计入配额（配额只算 active），等回收站到点
	// 清理时又会被物理删掉。所以这里对所有人一律只认 active 文件 ——
	// 与函数注释、docs/design.md 同一口径。
	if f.Status != model.StatusActive {
		return nil, fmt.Errorf("%w: 文件已被删除，无法设置有效期", store.ErrState)
	}
	var expiresAt *time.Time
	if permanent {
		// 已经是永久的文件不再重复计入"新增占用"：它本来就算在 used 里，
		// 再算一遍会让"对同一个文件重发一次请求"（超时重试、双标签页、
		// 列表里 permanent 还没刷新的旧行）被 409 拒掉 —— 而 PUT 应当幂等。
		add := int64(0)
		if f.ExpiresAt != nil {
			add = f.SizeBytes
		}
		if err := s.ensurePermanentFits(ctx, 1, add); err != nil {
			return nil, err
		}
		expiresAt = nil
	} else {
		at := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, s.set.Get().RetentionDays)
		expiresAt = &at
	}
	if _, err := s.store.SetExpiry(ctx, id, expiresAt); err != nil {
		return nil, err
	}
	updated, err := s.store.GetFile(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.decorate(updated, actor)
	return &out, nil
}

// SetPermanentUnderFolder 把某个目录（**递归到文件**）下的所有文件设为永久或改为有期限。
//
// 只影响目录树下**当前是 active** 的文件：回收站里的不动，避免把一堆
// 待清理的文件也变成永久。
//
// 返回受影响的文件数。目录本身没有"永久"这个属性 —— 永久是文件的属性，
// 目录只是个范围，因此这里只改文件、不动 folders 表。
func (s *Service) SetPermanentUnderFolder(ctx context.Context, ownerID int64, dirRel string, permanent bool, actor *model.User) (int64, error) {
	var expiresAt *time.Time
	if permanent {
		// 配额按"本次会新增多少"算：已经是永久的不重复计入。
		// 顺便取回文件数，供配额不足的提示写明"涉及几个文件"。
		count, add, err := s.store.PermanentizableStats(ctx, ownerID, dirRel)
		if err != nil {
			return 0, err
		}
		if err := s.ensurePermanentFits(ctx, int(count), add); err != nil {
			return 0, err
		}
	} else {
		at := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, s.set.Get().RetentionDays)
		expiresAt = &at
	}
	return s.store.SetExpiryUnderPath(ctx, ownerID, dirRel, expiresAt)
}

// ensurePermanentFits 校验"再永久化 addBytes"是否放得下。
//
// 配额不足时返回的错误里带完整数字（已用 / 上限 / 还差多少），
// 让前端能直接告诉用户"还差多少" —— 只说"空间不足"用户不知道该怎么办。
func (s *Service) ensurePermanentFits(ctx context.Context, fileCount int, addBytes int64) error {
	cfg := s.set.Get()
	if !cfg.PermanentEnabled() {
		return ErrPermanentDisabled
	}
	used, err := s.store.PermanentUsage(ctx)
	if err != nil {
		return err
	}
	// addBytes <= 0（比如目录里全是已永久的文件、或空目录）时不需要占用新额度，
	// 但仍要过 enabled 这道闸：关闭永久功能时不该允许任何"设为永久"的操作。
	if addBytes > 0 && !cfg.PermanentFits(used, addBytes) {
		quota := cfg.PermanentQuotaBytes()
		short := used + addBytes - quota
		if short < 0 {
			short = 0
		}
		return fmt.Errorf("%w：已用 %s / 上限 %s，还需 %s（本次涉及 %d 个文件）",
			ErrPermanentQuota, fmtBytesCN(used), fmtBytesCN(quota), fmtBytesCN(short), fileCount)
	}
	return nil
}

// fmtBytesCN 生成人类可读的体积文案（与 handler.fmtBytes 同一套进位规则）。
//
// 放在 files 包而不是复用 handler 的：错误信息要在这里就拼好，
// 而 handler 不能反向依赖 —— 两处实现保持一致即可（口径都是 1024 进位）。
func fmtBytesCN(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// cleanupEmptyDir 在文件被删除后清理其所在的空目录（数据根目录本身不删）。
func (s *Service) cleanupEmptyDir(relPath string) {
	idx := strings.LastIndex(relPath, "/")
	if idx <= 0 {
		return
	}
	dir := relPath[:idx]
	// 只清理 users/<id> 这一层，绝不动数据根目录。
	if !strings.HasPrefix(dir, "users/") || strings.Count(dir, "/") != 1 {
		return
	}
	_ = s.st.PruneEmptyDirs(dir)
}

// Owners 返回用户目录聚合（供前端左侧目录树）。
// viewerID 决定返回结果里的 pinned 标记与置顶排序（每人各有一份置顶）。
func (s *Service) Owners(ctx context.Context, viewerID int64) ([]model.OwnerAggregate, error) {
	return s.store.ListOwners(ctx, viewerID)
}

// PinOwner 置顶 / 取消置顶某人的目录（仅影响 viewerID 自己的视图）。
func (s *Service) PinOwner(ctx context.Context, viewerID, targetID int64, pinned bool) error {
	if viewerID == targetID {
		// 自己的文件在"我的文件"里始终可达，置顶自己没有意义。
		// 明确报错而不是静默成功，避免前端以为置顶生效却在列表里看不到变化。
		return ErrCannotPinSelf
	}
	if _, err := s.store.GetUserByID(ctx, targetID); err != nil {
		return err
	}
	if pinned {
		return s.store.PinOwner(ctx, viewerID, targetID)
	}
	return s.store.UnpinOwner(ctx, viewerID, targetID)
}

// normalizePage 规范化分页参数。
func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
