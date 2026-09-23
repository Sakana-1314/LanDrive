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
		OwnerID:  ownerID,
		Status:   status,
		Keyword:  opt.Keyword,
		Ext:      opt.Ext,
		Sort:     opt.Sort,
		Order:    opt.Order,
		Page:     page,
		PageSize: pageSize,
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

// decorate 计算 days_left / is_mine / can_edit。
func (s *Service) decorate(f *model.File, actor *model.User) model.File {
	out := *f
	now := time.Now().UTC()
	out.DaysLeft = int(math.Ceil(out.ExpiresAt.Sub(now).Hours() / 24))
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
	set := s.set.Get()
	expiresAt := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, set.RetentionDays)
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
func (s *Service) Owners(ctx context.Context) ([]model.OwnerAggregate, error) {
	return s.store.ListOwners(ctx)
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
