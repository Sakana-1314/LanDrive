package store

import (
	"context"
	"database/sql"
	"fmt"

	"lan-drive/internal/model"
)

// folderCols 带上属主工号：目录的磁盘路径是 users/<工号>/<path>，
// 任何需要拼磁盘路径的地方（分享解析、目录下载、改名）都要用到它，
// 少了它就会拼出 users//... 这种非法路径。
const folderCols = `f.id, f.owner_id, f.parent_id, f.name, f.path, f.status,
	f.created_at, f.updated_at, u.name, u.employee_no`

func scanFolder(sc interface {
	Scan(dest ...any) error
}) (*model.Folder, error) {
	var f model.Folder
	var parent sql.NullInt64
	if err := sc.Scan(&f.ID, &f.OwnerID, &parent, &f.Name, &f.Path, &f.Status,
		&f.CreatedAt, &f.UpdatedAt, &f.OwnerName, &f.OwnerEmployeeNo); err != nil {
		return nil, err
	}
	f.Tombstone = f.Status == model.FolderDeleted
	f.ParentID = nullID(parent)
	f.CreatedAt = f.CreatedAt.UTC()
	f.UpdatedAt = f.UpdatedAt.UTC()
	return &f, nil
}

// CreateFolder 插入一条目录记录。
func (s *Store) CreateFolder(ctx context.Context, f *model.Folder) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO folders (owner_id, parent_id, name, path) VALUES (?, ?, ?, ?)`,
		f.OwnerID, f.ParentID, f.Name, f.Path)
	if err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("%w: 该目录下已存在同名文件夹", ErrConflict)
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	f.ID = id
	return nil
}

// GetFolder 按主键查询。
func (s *Store) GetFolder(ctx context.Context, id int64) (*model.Folder, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders f JOIN users u ON u.id = f.owner_id WHERE f.id = ?`, id)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return f, err
}

// CountFoldersByOwner 统计某人的目录数（防滥用上限用）。
func (s *Store) CountFoldersByOwner(ctx context.Context, ownerID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

// ListChildFolders 列出某层直接子目录（parentID 为 nil 表示根层的目录）。
// 附带每个目录的直接文件数与占用，便于前端直接展示。
func (s *Store) ListChildFolders(ctx context.Context, ownerID int64, parentID *int64) ([]model.Folder, error) {
	// parent_id 可空，不能用 = NULL 比较，按情况构造条件。
	// 注意参数顺序：这里的 ? 都在 SELECT 的 JOIN 子查询里，
	// 而 WHERE 中的 parentID 参数必须排在最后，与 SQL 中出现的顺序一致。
	cond := "f.parent_id IS NULL"
	whereArgs := []any{}
	if parentID != nil {
		cond = "f.parent_id = ?"
		whereArgs = append(whereArgs, *parentID)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+folderCols+`,
			COALESCE(fc.cnt, 0), COALESCE(fc.bytes, 0), COALESCE(sc.cnt, 0)
		 FROM folders f
		 JOIN users u ON u.id = f.owner_id
		 LEFT JOIN (
			SELECT folder_id, COUNT(*) AS cnt, CAST(COALESCE(SUM(size_bytes),0) AS SIGNED) AS bytes
			FROM files WHERE owner_id = ? AND status = ? GROUP BY folder_id
		 ) fc ON fc.folder_id = f.id
		 LEFT JOIN (
			SELECT parent_id, COUNT(*) AS cnt FROM folders GROUP BY parent_id
		 ) sc ON sc.parent_id = f.id
		 WHERE f.owner_id = ? AND f.status = 'active' AND `+cond+`
		 ORDER BY f.name ASC`,
		append([]any{ownerID, model.StatusActive, ownerID}, whereArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		var parent sql.NullInt64
		// 顺序必须与 folderCols + 3 个聚合列一致。
		if err := rows.Scan(&f.ID, &f.OwnerID, &parent, &f.Name, &f.Path, &f.Status,
			&f.CreatedAt, &f.UpdatedAt, &f.OwnerName, &f.OwnerEmployeeNo,
			&f.FileCount, &f.UsedBytes, &f.SubFolderCount); err != nil {
			return nil, err
		}
		f.ParentID = nullID(parent)
		f.CreatedAt = f.CreatedAt.UTC()
		f.UpdatedAt = f.UpdatedAt.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListAllFoldersByOwner 返回某人的全部目录（用于构建面包屑与校验归属）。
func (s *Store) ListAllFoldersByOwner(ctx context.Context, ownerID int64) ([]model.Folder, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+folderCols+` FROM folders f JOIN users u ON u.id = f.owner_id WHERE f.owner_id = ? AND f.status = 'active' ORDER BY f.path ASC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// ListFolderAncestors 返回从根到该目录的祖先链（含自己），用于面包屑。
func (s *Store) ListFolderAncestors(ctx context.Context, ownerID, folderID int64) ([]model.Folder, error) {
	// 用 path 前缀匹配避免递归查询（MySQL 8 与 MariaDB 都支持）：
	// 目标目录的 path 是 "a/b/c"，则祖先的 path 必是 "a/b/c" 或其前缀且以 / 结尾。
	target, err := s.GetFolder(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if target.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+folderCols+` FROM folders f JOIN users u ON u.id = f.owner_id
		 WHERE f.owner_id = ? AND f.status = 'active' AND (f.path = ? OR ? LIKE CONCAT(f.path, '/%'))
		 ORDER BY LENGTH(f.path) ASC`,
		ownerID, target.Path, target.Path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// FolderNameExists 判断同一父目录下是否已有同名文件夹（excludeID 用于改名时排除自己）。
func (s *Store) FolderNameExists(ctx context.Context, ownerID int64, parentID *int64, name string, excludeID int64) (bool, error) {
	// 条件与占位符必须成对出现：parentID 为 nil 时用 IS NULL（**不能**再追加参数，
	// 否则占位符与参数个数不匹配，报 "expected N arguments, got M"）。
	cond := "parent_id IS NULL"
	args := []any{ownerID, name, excludeID}
	if parentID != nil {
		cond = "parent_id = ?"
		args = append(args, *parentID)
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM folders WHERE owner_id = ? AND status = 'active' AND name = ? AND id <> ? AND `+cond,
		args...).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RenameFolderPathWithParent 改名/移动目录：更新自身（含 parent_id），
// 并级联更新所有子孙的 path 前缀。
//
// 必须在事务里执行：唯一键 uk_folders_owner_path 要求中途不出现重复路径。
// 子孙的 path 必然以旧 path + "/" 开头，因此用 CONCAT + SUBSTRING 做前缀替换
// （不用 REPLACE，避免路径中恰好出现同名片段被误替换）。
func (s *Store) RenameFolderPathWithParent(ctx context.Context, ownerID, folderID int64, newParentID *int64, newName, newPath string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var oldPath string
	if err := tx.QueryRowContext(ctx,
		`SELECT path FROM folders WHERE id = ? AND owner_id = ? FOR UPDATE`,
		folderID, ownerID).Scan(&oldPath); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}

	// 先改自身（parent_id 也要改：移动目录时会换父级）。
	if _, err := tx.ExecContext(ctx,
		`UPDATE folders SET name = ?, path = ?, parent_id = ? WHERE id = ? AND owner_id = ?`,
		newName, newPath, newParentID, folderID, ownerID); err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("%w: 该目录下已存在同名文件夹", ErrConflict)
		}
		return err
	}

	// 再级联改子孙：把 path 前缀从 oldPath 换成 newPath。
	// CONCAT + SUBSTRING 而非 REPLACE，避免路径中恰好出现同名片段被误替换。
	// 同 MoveFilesPathPrefix：偏移按字符算（CHAR_LENGTH），并补回分隔符 '/'。
	if _, err := tx.ExecContext(ctx,
		`UPDATE folders
		 SET path = CONCAT(?, '/', SUBSTRING(path, CHAR_LENGTH(?) + 2))
		 WHERE owner_id = ? AND path LIKE ? AND id <> ?`,
		newPath, oldPath, ownerID, escapeLike(oldPath)+"/%", folderID); err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("%w: 目标路径下已存在同名文件夹", ErrConflict)
		}
		return err
	}

	return tx.Commit()
}

// MoveFilesPathPrefix 把某目录树下所有文件的 rel_path 前缀从 oldDir 换成 newDir。
// 由服务层在目录改名/移动时调用（它知道完整的磁盘目录前缀）。
func (s *Store) MoveFilesPathPrefix(ctx context.Context, ownerID int64, oldDirRel, newDirRel string) error {
	if oldDirRel == newDirRel {
		return nil
	}
	// 两个易错点，都实测踩过：
	//   1. 用 CHAR_LENGTH 而不是 Go 的 len() —— SUBSTRING 在 MySQL 里按**字符**
	//      计数，len() 是字节数；目录名含中文时用字节偏移会把路径切碎。
	//   2. CONCAT 时必须补回分隔符 '/'，否则会拼成 "旧名27.txt" 而不是
	//      "旧名/27.txt"，文件就再也匹配不上自己目录的前缀。
	_, err := s.db.ExecContext(ctx,
		`UPDATE files SET rel_path = CONCAT(?, '/', SUBSTRING(rel_path, CHAR_LENGTH(?) + 2))
		 WHERE owner_id = ? AND rel_path LIKE ?`,
		newDirRel, oldDirRel, ownerID, escapeLike(oldDirRel)+"/%")
	if err != nil && isDuplicate(err) {
		return fmt.Errorf("%w: 目标位置已存在同名文件", ErrConflict)
	}
	return err
}

// ListFoldersUnderPath 返回某路径及其所有子孙目录（用于目录删除/改名）。
func (s *Store) ListFoldersUnderPath(ctx context.Context, ownerID int64, path string) ([]model.Folder, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+folderCols+` FROM folders f JOIN users u ON u.id = f.owner_id
		 WHERE f.owner_id = ? AND (f.path = ? OR f.path LIKE ?)
		 ORDER BY LENGTH(f.path) DESC`,
		ownerID, path, escapeLike(path)+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// SoftDeleteFolderTree 软删除目录及其所有子孙。
//
// 不删行，而是把 status 置为 deleted、并把 path 追加墓碑后缀 ":<id>"：
//   - 分享指向目录记录，"文件夹已被删除"要能被解析出来（删行会退化成"链接无效"）；
//   - uk_folders_owner_path 是 (owner_id, path) 唯一键，不加后缀就无法再建同名目录。
//
// 目录下的文件由服务层先软删，语义与删文件保持一致（都进回收站、都可恢复）。
func (s *Store) SoftDeleteFolderTree(ctx context.Context, ownerID int64, path string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE folders
		 SET status = ?, path = CONCAT(path, ':', id)
		 WHERE owner_id = ? AND status = ? AND (path = ? OR path LIKE ?)`,
		model.FolderDeleted, ownerID, model.FolderActive, path, escapeLike(path)+"/%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SoftDeleteFilesUnderPath 把某目录树下所有 active 文件软删（进回收站）。
// 删除目录时调用：文件仍可被管理员恢复，符合"软删除就行了"的既有约定。
func (s *Store) SoftDeleteFilesUnderPath(ctx context.Context, ownerID int64, dirRel string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET status = ?, purge_at = NULL
		 WHERE owner_id = ? AND status = ? AND rel_path LIKE ?`,
		model.StatusTrashed, ownerID, model.StatusActive, escapeLike(dirRel)+"/%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountFilesUnderFolder 统计某目录（含子孙）下的 active 文件数。
func (s *Store) CountFilesUnderFolder(ctx context.Context, ownerID int64, dirRel string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM files WHERE owner_id = ? AND status = ? AND rel_path LIKE ?`,
		ownerID, model.StatusActive, escapeLike(dirRel)+"/%").Scan(&n)
	return n, err
}
