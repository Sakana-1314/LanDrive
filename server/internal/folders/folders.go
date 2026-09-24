// Package folders 实现文件夹：创建、改名/移动、删除。
//
// 磁盘是**真实层级**：目录 `报表/2026`（folders.path）对应磁盘
// users/<工号>/报表/2026，目录内文件落在该目录下，文件名仍是 <fileID><ext>。
// 因此改名/移动目录必须同时(a) 改数据库 path 前缀、(b) 移动磁盘目录、
// (c) 改写其下所有文件的 rel_path，三者必须一致，否则会与一致性扫描打架。
package folders

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lan-drive/internal/model"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// ErrForbidden 无权操作该目录。
var ErrForbidden = errors.New("无权操作该文件夹")

// ErrInvalidName 目录名不合法。
var ErrInvalidName = errors.New("文件夹名不合法")

// maxDepth 目录最大层级。防止把路径无限拉长（也会撑爆 VARCHAR(512)）。
const maxDepth = 16

// maxFoldersPerUser 单账号目录数上限，防滥用。
const maxFoldersPerUser = 2000

// Service 目录业务服务。
type Service struct {
	store *store.Store
	st    *storage.Storage
}

// New 构造服务。
func New(s *store.Store, st *storage.Storage) *Service {
	return &Service{store: s, st: st}
}

// canModify 属主校验：管理员可管理任意目录，普通用户只能动自己的。
func canModify(actor *model.User, ownerID int64) bool {
	if actor == nil {
		return false
	}
	return actor.IsAdmin() || actor.ID == ownerID
}

// ListInput 列目录的入参。
type ListInput struct {
	Actor *model.User
	// OwnerID 看谁的目录；0 表示看自己。
	OwnerID int64
	// FolderID 进入哪个目录；0 表示根层。
	FolderID int64
}

// Listing 一层目录的内容。
type Listing struct {
	OwnerID  int64          `json:"owner_id"`
	FolderID int64          `json:"folder_id"`
	Folders  []model.Folder `json:"folders"`
	// Breadcrumb 从根到当前目录的路径，供前端面包屑。
	Breadcrumb []model.Folder `json:"breadcrumb"`
	// Current 当前目录（根层时为 nil）。
	Current *model.Folder `json:"current"`
}

// List 列出某层目录的子目录（文件由 files 服务按 folder_id 查）。
func (s *Service) List(ctx context.Context, in ListInput) (*Listing, error) {
	if in.Actor == nil {
		return nil, ErrForbidden
	}
	ownerID := in.OwnerID
	if ownerID == 0 {
		ownerID = in.Actor.ID
	}

	out := &Listing{OwnerID: ownerID, FolderID: in.FolderID, Folders: []model.Folder{}, Breadcrumb: []model.Folder{}}

	// 根层：parent 为空。
	var parent *int64
	if in.FolderID > 0 {
		f, err := s.store.GetFolder(ctx, in.FolderID)
		if err != nil {
			return nil, err
		}
		if f.OwnerID != ownerID {
			return nil, store.ErrNotFound
		}
		out.Current = f
		parent = &f.ID
		// 面包屑
		anc, err := s.store.ListFolderAncestors(ctx, ownerID, f.ID)
		if err != nil {
			return nil, err
		}
		out.Breadcrumb = anc
	}

	subs, err := s.store.ListChildFolders(ctx, ownerID, parent)
	if err != nil {
		return nil, err
	}
	out.Folders = subs
	return out, nil
}

// CreateInput 建目录的入参。
type CreateInput struct {
	Actor    *model.User
	OwnerID  int64 // 0 = 自己
	ParentID int64 // 0 = 根层
	Name     string
}

// Create 新建目录（同时创建磁盘目录）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*model.Folder, error) {
	if in.Actor == nil {
		return nil, ErrForbidden
	}
	ownerID := in.OwnerID
	if ownerID == 0 {
		ownerID = in.Actor.ID
	}
	if !canModify(in.Actor, ownerID) {
		return nil, fmt.Errorf("%w: 只能在自己的目录里新建文件夹", ErrForbidden)
	}

	name := storage.SanitizeFolderName(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 名称不能为空，且不能是 . 或 ..", ErrInvalidName)
	}

	owner, err := s.store.GetUserByID(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	// 解析父目录与目标 path
	var parentID *int64
	parentPath := ""
	if in.ParentID > 0 {
		pf, err := s.store.GetFolder(ctx, in.ParentID)
		if err != nil {
			return nil, err
		}
		if pf.OwnerID != ownerID {
			return nil, store.ErrNotFound
		}
		parentPath = pf.Path
		pid := pf.ID
		parentID = &pid
	}

	fullPath := name
	if parentPath != "" {
		fullPath = parentPath + "/" + name
	}
	if strings.Count(fullPath, "/")+1 > maxDepth {
		return nil, fmt.Errorf("%w: 目录层级不能超过 %d 层", ErrInvalidName, maxDepth)
	}

	// 同层重名检查（数据库唯一键是 (owner_id, path)，这里给出更友好的提示）
	exists, err := s.store.FolderNameExists(ctx, ownerID, parentID, name, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: 该位置已存在同名文件夹", store.ErrConflict)
	}

	n, err := s.store.CountFoldersByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if n >= maxFoldersPerUser {
		return nil, fmt.Errorf("%w: 文件夹数量已达上限（%d）", store.ErrInvalidInput, maxFoldersPerUser)
	}

	f := &model.Folder{OwnerID: ownerID, ParentID: parentID, Name: name, Path: fullPath}
	if err := s.store.CreateFolder(ctx, f); err != nil {
		return nil, err
	}

	// 建磁盘目录。失败则回滚记录，避免出现"有记录没目录"。
	dirRel, err := storage.FolderDirRel(storage.UserDirRel(owner.EmployeeNo), fullPath)
	if err != nil {
		_, _ = s.store.SoftDeleteFolderTree(ctx, ownerID, fullPath)
		return nil, err
	}
	if err := s.st.EnsureDir(dirRel); err != nil {
		_, _ = s.store.SoftDeleteFolderTree(ctx, ownerID, fullPath)
		return nil, err
	}
	return f, nil
}

// RenameInput 改名/移动的入参。
type RenameInput struct {
	Actor *model.User
	ID    int64
	// NewName 新目录名；若只想移动不改名，传当前名。
	NewName string
	// NewParentID 目标父目录；0 表示移到根层。与当前父目录相同即只改名。
	NewParentID int64
}

// Rename 改名或移动目录（含磁盘与文件 rel_path 同步）。
//
// 三种情况都要照顾到：只改名、只移动、同时改名与移动。
func (s *Service) Rename(ctx context.Context, in RenameInput) (*model.Folder, error) {
	if in.Actor == nil {
		return nil, ErrForbidden
	}
	f, err := s.store.GetFolder(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if !canModify(in.Actor, f.OwnerID) {
		return nil, fmt.Errorf("%w: 只能修改自己的文件夹", ErrForbidden)
	}
	owner, err := s.store.GetUserByID(ctx, f.OwnerID)
	if err != nil {
		return nil, err
	}

	name := storage.SanitizeFolderName(in.NewName)
	if name == "" {
		return nil, fmt.Errorf("%w: 名称不能为空，且不能是 . 或 ..", ErrInvalidName)
	}

	// 目标父目录
	var newParentID *int64
	parentPath := ""
	if in.NewParentID > 0 {
		pf, err := s.store.GetFolder(ctx, in.NewParentID)
		if err != nil {
			return nil, err
		}
		if pf.OwnerID != f.OwnerID {
			return nil, store.ErrNotFound
		}
		// 不能把目录移到自己或自己的子孙下（会形成环并把 path 前缀搞乱）
		if pf.ID == f.ID || strings.HasPrefix(pf.Path+"/", f.Path+"/") {
			return nil, fmt.Errorf("%w: 不能把文件夹移动到它自己或其子目录中", ErrInvalidName)
		}
		parentPath = pf.Path
		pid := pf.ID
		newParentID = &pid
	}

	newPath := name
	if parentPath != "" {
		newPath = parentPath + "/" + name
	}
	if newPath == f.Path {
		return f, nil // 没有任何变化
	}

	// 同层重名
	exists, err := s.store.FolderNameExists(ctx, f.OwnerID, newParentID, name, f.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: 目标位置已存在同名文件夹", store.ErrConflict)
	}

	oldDirRel, err := storage.FolderDirRel(storage.UserDirRel(owner.EmployeeNo), f.Path)
	if err != nil {
		return nil, err
	}
	newDirRel, err := storage.FolderDirRel(storage.UserDirRel(owner.EmployeeNo), newPath)
	if err != nil {
		return nil, err
	}

	// 1) 数据库：自身 + 子孙 path、以及其下文件的 rel_path
	if err := s.store.RenameFolderPathWithParent(ctx, f.OwnerID, f.ID, newParentID, name, newPath); err != nil {
		return nil, err
	}
	if err := s.store.MoveFilesPathPrefix(ctx, f.OwnerID, oldDirRel, newDirRel); err != nil {
		return nil, err
	}
	// 2) 磁盘：移动目录（RenameDir 内部会创建父目录）
	if err := s.st.RenameDir(oldDirRel, newDirRel); err != nil {
		return nil, err
	}

	updated, err := s.store.GetFolder(ctx, f.ID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete 删除目录：把其下所有文件软删（进回收站），并软删目录记录。
//
// **磁盘字节必须保留**，与"删除文件"的语义保持一致（进回收站 → 管理员可恢复 →
// 到期才物理清理）。早先的实现在这里顺手 RemoveAll 了磁盘目录，结果是：
//   - 回收站里的记录指向已经不存在的文件，"恢复"恢复出来是个坏记录；
//   - 一致性扫描报"数据库有、磁盘无"，属真实数据破损。
//
// 空目录残留无害（保持目录结构，便于恢复后原样放回），最终由文件的
// 物理清理与空目录剪枝处理。
func (s *Service) Delete(ctx context.Context, actor *model.User, id int64) (int64, error) {
	if actor == nil {
		return 0, ErrForbidden
	}
	f, err := s.store.GetFolder(ctx, id)
	if err != nil {
		return 0, err
	}
	if !canModify(actor, f.OwnerID) {
		return 0, fmt.Errorf("%w: 只能删除自己的文件夹", ErrForbidden)
	}
	owner, err := s.store.GetUserByID(ctx, f.OwnerID)
	if err != nil {
		return 0, err
	}
	dirRel, err := storage.FolderDirRel(storage.UserDirRel(owner.EmployeeNo), f.Path)
	if err != nil {
		return 0, err
	}

	// 1) 先把文件软删（可恢复），再删目录记录。
	//    顺序不能反：先删记录会让文件失去归属，变成"根目录下的孤儿"。
	n, err := s.store.SoftDeleteFilesUnderPath(ctx, f.OwnerID, dirRel)
	if err != nil {
		return 0, err
	}
	// 2) 软删除目录记录（子树一并软删，保留记录以便分享能报"已被删除"）。
	if _, err := s.store.SoftDeleteFolderTree(ctx, f.OwnerID, f.Path); err != nil {
		return 0, err
	}
	// 刻意**不删磁盘目录**：其中的文件只是进了回收站，字节要留着才能恢复。
	_ = dirRel
	return n, nil
}

// FolderDirRel 供上层（如 files 查询、分享）复用：算出某目录的磁盘相对路径。
func FolderDirRel(ownerEmployeeNo string, folder *model.Folder) (string, error) {
	if folder == nil {
		return storage.UserDirRel(ownerEmployeeNo), nil
	}
	return storage.FolderDirRel(storage.UserDirRel(ownerEmployeeNo), folder.Path)
}
