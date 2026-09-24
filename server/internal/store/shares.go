package store

import (
	"context"
	"database/sql"
	"time"

	"lan-drive/internal/model"
)

// shareCols 分享表字段 + 创建者信息（列表页要显示是谁分享的）。
const shareCols = `s.id, s.token, s.owner_id, s.target_type, s.file_id, s.folder_id,
	s.expire_days, s.expires_at, s.view_count, s.created_at, s.updated_at,
	u.name, u.employee_no`

func scanShare(sc interface {
	Scan(dest ...any) error
}) (*model.Share, error) {
	var sh model.Share
	var fileID, folderID, expireDays sql.NullInt64
	var expiresAt sql.NullTime
	if err := sc.Scan(&sh.ID, &sh.Token, &sh.OwnerID, &sh.TargetType, &fileID, &folderID,
		&expireDays, &expiresAt, &sh.ViewCount, &sh.CreatedAt, &sh.UpdatedAt,
		&sh.OwnerName, &sh.OwnerEmployeeNo); err != nil {
		return nil, err
	}
	sh.FileID = nullID(fileID)
	sh.FolderID = nullID(folderID)
	if expireDays.Valid {
		v := int(expireDays.Int64)
		sh.ExpireDays = &v
	}
	sh.ExpiresAt = nullTime(expiresAt)
	sh.CreatedAt = sh.CreatedAt.UTC()
	sh.UpdatedAt = sh.UpdatedAt.UTC()
	return &sh, nil
}

// CreateShare 插入一条分享。
func (s *Store) CreateShare(ctx context.Context, sh *model.Share) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO shares (token, owner_id, target_type, file_id, folder_id, expire_days, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sh.Token, sh.OwnerID, sh.TargetType, sh.FileID, sh.FolderID, sh.ExpireDays, sh.ExpiresAt)
	if err != nil {
		if isDuplicate(err) {
			return ErrConflict
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	sh.ID = id
	return nil
}

// GetShareByToken 按 token 查询（免登录访问的唯一入口）。
func (s *Store) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+shareCols+` FROM shares s JOIN users u ON u.id = s.owner_id WHERE s.token = ?`, token)
	sh, err := scanShare(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return sh, err
}

// GetShareByID 按主键查询。
func (s *Store) GetShareByID(ctx context.Context, id int64) (*model.Share, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+shareCols+` FROM shares s JOIN users u ON u.id = s.owner_id WHERE s.id = ?`, id)
	sh, err := scanShare(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return sh, err
}

// ShareListQuery 分享列表的筛选条件。
type ShareListQuery struct {
	// OwnerID 限定创建者（0 表示不限定，即"所有人的分享"）。
	OwnerID  int64
	Page     int
	PageSize int
}

// ListShares 分页列出分享。
//
// 需求是"所有人都能看到所有人创建的分享链接"，因此默认**不过滤 owner_id**；
// 列表里带创建者姓名/工号，让人知道是谁分享的。
// 目标名与目标是否被删除由服务层补全（要跨 files/folders 两张表取）。
func (s *Store) ListShares(ctx context.Context, q ShareListQuery) ([]model.Share, int64, error) {
	where := "1=1"
	args := []any{}
	if q.OwnerID > 0 {
		where += " AND s.owner_id = ?"
		args = append(args, q.OwnerID)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shares s WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+shareCols+` FROM shares s JOIN users u ON u.id = s.owner_id
		 WHERE `+where+` ORDER BY s.id DESC LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]model.Share, 0, q.PageSize)
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *sh)
	}
	return out, total, rows.Err()
}

// DeleteShare 删除一条分享（撤销）。返回删除条数，0 表示不存在。
func (s *Store) DeleteShare(ctx context.Context, id int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE id = ?`, id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// IncrementShareView 原子自增访问次数。
//
// 用 SQL 自增而不是"读出来 +1 再写回"：并发访问时后者会丢计数。
func (s *Store) IncrementShareView(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shares SET view_count = view_count + 1 WHERE id = ?`, id)
	return err
}

// PurgeExpiredShares 删除过期超过 graceDays 天的分享，返回删除条数。
// 永久分享（expires_at IS NULL）不受影响。
func (s *Store) PurgeExpiredShares(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM shares WHERE expires_at IS NOT NULL AND expires_at < ?`, before.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountSharesByTarget 统计某个目标上还有多少条分享（撤销目标前的提示用）。
func (s *Store) CountSharesByTarget(ctx context.Context, targetType string, targetID int64) (int64, error) {
	col := "file_id"
	if targetType == model.ShareTargetFolder {
		col = "folder_id"
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM shares WHERE `+col+` = ?`, targetID).Scan(&n)
	return n, err
}

// GetFileForShare 读取分享指向的文件记录（含 trashed），
// 用于判断"目标是否已被删除"而**不是**直接返回 404。
func (s *Store) GetFileForShare(ctx context.Context, id int64) (*model.File, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id WHERE f.id = ?`, id)
	f, err := scanFile(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return f, err
}

// CountFilesInFolder 统计某目录下（含子孙）的 active 文件数与总大小。
// 目录分享展示与打包下载用。
func (s *Store) CountFilesInFolder(ctx context.Context, ownerID int64, dirRel string) (int64, int64, error) {
	var n, bytes int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM files
		 WHERE owner_id = ? AND status = ? AND rel_path LIKE ?`,
		ownerID, model.StatusActive, escapeLike(dirRel)+"/%").Scan(&n, &bytes)
	return n, bytes, err
}

// ListFilesInFolder 列出某目录下（含子孙）的 active 文件，供目录分享打包下载。
func (s *Store) ListFilesInFolder(ctx context.Context, ownerID int64, dirRel string) ([]model.File, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id
		 WHERE f.owner_id = ? AND f.status = ? AND f.rel_path LIKE ?
		 ORDER BY f.rel_path ASC`,
		ownerID, model.StatusActive, escapeLike(dirRel)+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}
