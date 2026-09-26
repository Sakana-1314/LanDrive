package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"lan-drive/internal/model"
)

const fileCols = `f.id, f.owner_id, f.folder_id, f.original_name, f.ext, f.size_bytes, f.mime, f.sha256,
	f.rel_path, f.status, f.expires_at, f.deleted_at, f.purge_at, f.created_at, f.updated_at,
	u.name, u.employee_no`

func scanFile(sc interface {
	Scan(dest ...any) error
}) (*model.File, error) {
	var f model.File
	var deleted, purge, expires sql.NullTime
	if err := sc.Scan(&f.ID, &f.OwnerID, &f.FolderID, &f.OriginalNam, &f.Ext, &f.SizeBytes, &f.Mime, &f.SHA256,
		&f.RelPath, &f.Status, &expires, &deleted, &purge, &f.CreatedAt, &f.UpdatedAt,
		&f.OwnerName, &f.OwnerEmployeeNo); err != nil {
		return nil, err
	}
	// expires_at 为 NULL = 永久，scan 出来是 Valid=false，转成 nil 指针即可。
	f.ExpiresAt = nullTime(expires)
	f.CreatedAt = f.CreatedAt.UTC()
	f.UpdatedAt = f.UpdatedAt.UTC()
	f.DeletedAt = nullTime(deleted)
	f.PurgeAt = nullTime(purge)
	return &f, nil
}

// CreateFile 插入一条文件记录（调用方必须先成功落盘）。
func (s *Store) CreateFile(ctx context.Context, f *model.File) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO files (owner_id, folder_id, original_name, ext, size_bytes, mime, sha256,
			rel_path, status, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.OwnerID, f.FolderID, f.OriginalNam, f.Ext, f.SizeBytes, f.Mime, f.SHA256, f.RelPath,
		f.Status, expiresArg(f.ExpiresAt))
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
	f.ID = id
	return nil
}

// GetFile 按主键查询文件（含属主姓名/工号）。
func (s *Store) GetFile(ctx context.Context, id int64) (*model.File, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id WHERE f.id = ?`, id)
	f, err := scanFile(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return f, err
}

// FileQuery 描述文件列表的检索条件。
type FileQuery struct {
	OwnerID int64 // 0 表示不限
	// FolderID 只看某个文件夹内的文件（>0 生效）。
	FolderID int64
	// FolderRootOnly 只看根目录下的文件（folder_id = 0）。
	// 之所以需要这个开关：0 本身就是"根目录"这个合法取值，
	// 没法用 FolderID 的零值同时表达"不过滤"和"只根目录"。
	FolderRootOnly bool
	Status         string // active / trashed / all
	Keyword        string // 匹配文件名或属主姓名/工号
	Ext            string // 精确匹配扩展名（小写带点）
	Page           int
	PageSize       int
	Sort           string // created_at / size_bytes / expires_at / original_name / owner
	Order          string // asc / desc
}

// sortColumn 把前端排序键映射到白名单列，杜绝 SQL 注入。
func (q FileQuery) sortColumn() string {
	switch q.Sort {
	case "size_bytes":
		return "f.size_bytes"
	case "expires_at":
		return "f.expires_at"
	case "original_name":
		return "f.original_name"
	case "owner":
		return "u.name"
	case "created_at":
		fallthrough
	default:
		return "f.created_at"
	}
}

func (q FileQuery) order() string {
	if strings.EqualFold(q.Order, "asc") {
		return "ASC"
	}
	return "DESC"
}

// orderBy 组装 ORDER BY 子句。
//
// 关键：按到期时间排序时，**永久文件（expires_at IS NULL）固定排在最后**，
// 与升降序无关。MySQL 对 NULL 的默认处理是"ASC 在前、DESC 在后"，
// 于是升序时永久文件会冒到最前面 —— 用户看到"永久"排在一堆马上要到期的
// 文件之前，会以为它也快到期了。显式用 `IS NULL` 排出正确顺序。
// 不用 MySQL 专有的 `NULLS LAST`：MySQL 8 与 MariaDB 11 支持情况不一致。
func (q FileQuery) orderBy() string {
	col := q.sortColumn()
	if col == "f.expires_at" {
		return "f.expires_at IS NULL, " + col + " " + q.order()
	}
	return col + " " + q.order()
}

func (q FileQuery) conditions() (string, []any) {
	conds := []string{"1=1"}
	args := []any{}
	switch q.Status {
	case model.StatusActive:
		conds = append(conds, "f.status = ?")
		args = append(args, model.StatusActive)
	case model.StatusTrashed:
		conds = append(conds, "f.status = ?")
		args = append(args, model.StatusTrashed)
	case "all", "":
		// 不限状态
	default:
		conds = append(conds, "f.status = ?")
		args = append(args, model.StatusActive)
	}
	if q.OwnerID > 0 {
		conds = append(conds, "f.owner_id = ?")
		args = append(args, q.OwnerID)
	}
	// 目录过滤：FolderID > 0 只看该目录；FolderRootOnly 只看根目录下的文件
	// （0 也是合法的 folder_id，所以需要单独一个开关，不能只靠零值判断）。
	if q.FolderID > 0 {
		conds = append(conds, "f.folder_id = ?")
		args = append(args, q.FolderID)
	} else if q.FolderRootOnly {
		conds = append(conds, "f.folder_id = 0")
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		conds = append(conds, "(f.original_name LIKE ? OR u.name LIKE ? OR u.employee_no LIKE ?)")
		args = append(args, like, like, like)
	}
	if ext := strings.TrimSpace(q.Ext); ext != "" {
		conds = append(conds, "f.ext = ?")
		args = append(args, strings.ToLower(ext))
	}
	return strings.Join(conds, " AND "), args
}

// ListFiles 分页查询文件。
func (s *Store) ListFiles(ctx context.Context, q FileQuery) ([]model.File, int64, error) {
	where, args := q.conditions()

	var total int64
	countSQL := `SELECT COUNT(*) FROM files f JOIN users u ON u.id = f.owner_id WHERE ` + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sqlText := `SELECT ` + fileCols + ` FROM files f JOIN users u ON u.id = f.owner_id
		WHERE ` + where + ` ORDER BY ` + q.orderBy() + `, f.id DESC LIMIT ? OFFSET ?`
	listArgs := append(append([]any{}, args...), q.PageSize, (q.Page-1)*q.PageSize)

	rows, err := s.db.QueryContext(ctx, sqlText, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]model.File, 0, q.PageSize)
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *f)
	}
	return out, total, rows.Err()
}

// FileExtStat 某个扩展名的文件数与占用。
type FileExtStat struct {
	Ext       string `json:"ext"`
	FileCount int64  `json:"file_count"`
	Bytes     int64  `json:"bytes"`
}

// ListExtStats 统计 active 文件的扩展名分布（管理端用）。
func (s *Store) ListExtStats(ctx context.Context, limit int) ([]FileExtStat, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ext, COUNT(*), CAST(COALESCE(SUM(size_bytes),0) AS SIGNED) FROM files
		 WHERE status = ? GROUP BY ext ORDER BY COUNT(*) DESC LIMIT ?`, model.StatusActive, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileExtStat{}
	for rows.Next() {
		var e FileExtStat
		if err := rows.Scan(&e.Ext, &e.FileCount, &e.Bytes); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FinishFile 在合并写盘成功后写入最终元数据（最终相对路径、sha256、实际大小）。
//
// 上传流程先用占位 rel_path 取得主键（磁盘文件名 = 主键 + 扩展名），
// 合并完成后调用本方法把占位路径替换为最终路径。
func (s *Store) FinishFile(ctx context.Context, id int64, relPath, sha256 string, size int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET rel_path = ?, sha256 = ?, size_bytes = ? WHERE id = ?`,
		relPath, sha256, size, id)
	if err != nil {
		if isDuplicate(err) {
			return ErrConflict
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, gerr := s.GetFile(ctx, id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// RenameFile 重命名（仅改数据库中的原始文件名，磁盘名不变）。
func (s *Store) RenameFile(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET original_name = ? WHERE id = ? AND status = ?`,
		name, id, model.StatusActive)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// ClientFoundRows 语义下，行存在但值未变也算 matched；再查一次以区分「不存在」。
		if _, gerr := s.GetFile(ctx, id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// MarkTrashed 把文件置为回收站状态（软删除）。
func (s *Store) MarkTrashed(ctx context.Context, id int64, deletedAt, purgeAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET status = ?, deleted_at = ?, purge_at = ? WHERE id = ? AND status = ?`,
		model.StatusTrashed, deletedAt.UTC(), purgeAt.UTC(), id, model.StatusActive)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, gerr := s.GetFile(ctx, id); gerr != nil {
			return gerr
		}
		return ErrState
	}
	return nil
}

// SoftDeleteAllByOwner 把某账号名下所有 active 文件一次性软删除（进回收站），
// 返回受影响的条数。已在回收站里的不动（避免刷新 purge_at 延长保留期）。
//
// purge_at 传 NULL：这些文件马上就要随账号一起被物理清除，
// 不需要再走"回收站保留 N 天"的流程。
func (s *Store) SoftDeleteAllByOwner(ctx context.Context, ownerID int64, deletedAt time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET status = ?, deleted_at = ?, purge_at = NULL
		 WHERE owner_id = ? AND status = ?`,
		model.StatusTrashed, deletedAt.UTC(), ownerID, model.StatusActive)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RestoreFile 从回收站恢复，并把到期时间设为 expiresAt（nil = 永久）。
//
// 永久文件被软删后恢复，**到期时间保持 NULL**（仍永久）：软删只是状态变化，
// 不应该顺手把它降级成有期限 —— 恢复的语义是"回到删除前的样子"。
func (s *Store) RestoreFile(ctx context.Context, id int64, expiresAt *time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET status = ?, deleted_at = NULL, purge_at = NULL, expires_at = ?
		 WHERE id = ? AND status = ?`,
		model.StatusActive, expiresArg(expiresAt), id, model.StatusTrashed)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, gerr := s.GetFile(ctx, id); gerr != nil {
			return gerr
		}
		return ErrState
	}
	return nil
}

// DeleteFile 物理删除数据库记录，返回其相对路径（调用方已负责删磁盘文件）。
func (s *Store) DeleteFile(ctx context.Context, id int64) (string, error) {
	f, err := s.GetFile(ctx, id)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id); err != nil {
		return "", err
	}
	return f.RelPath, nil
}

// DeleteFileRow 只删除数据库记录（用于清理时磁盘文件已不存在的情况）。
func (s *Store) DeleteFileRow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

// ExpireFiles 把到期的 active 文件批量置为回收站状态，返回受影响记录。
func (s *Store) ExpireFiles(ctx context.Context, now time.Time, trashDays int) ([]model.File, error) {
	// 先查出待处理记录，便于逐个计算 purge_at 并记录日志。
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id
		 WHERE f.status = ? AND f.expires_at <= ?`, model.StatusActive, now.UTC())
	if err != nil {
		return nil, err
	}
	var list []model.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, *f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	purgeAt := now.UTC().AddDate(0, 0, trashDays)
	done := make([]model.File, 0, len(list))
	for _, f := range list {
		res, err := s.db.ExecContext(ctx,
			`UPDATE files SET status = ?, deleted_at = ?, purge_at = ?
			 WHERE id = ? AND status = ?`,
			model.StatusTrashed, now.UTC(), purgeAt, f.ID, model.StatusActive)
		if err != nil {
			return done, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			done = append(done, f)
		}
	}
	return done, nil
}

// ListPurgeable 返回已到物理删除时刻的回收站文件。
func (s *Store) ListPurgeable(ctx context.Context, now time.Time, limit int) ([]model.File, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id
		 WHERE f.status = ? AND f.purge_at IS NOT NULL AND f.purge_at <= ?
		 ORDER BY f.id ASC LIMIT ?`, model.StatusTrashed, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// ListFilesByOwner 返回某用户的全部文件（不限状态，用于删账号时清理磁盘）。
func (s *Store) ListFilesByOwner(ctx context.Context, ownerID int64) ([]model.File, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files f JOIN users u ON u.id = f.owner_id WHERE f.owner_id = ?`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// CountFilesByOwner 统计某用户的全部文件数（用于删除账号前的 409 提示）。
//
// 注意这里**不按 status 过滤**：删除账号前要提示「该账号还有 N 个文件」，
// 回收站里的也算。若要展示给用户看的"我的文件数"，请用 ActiveUsageByOwner。
func (s *Store) CountFilesByOwner(ctx context.Context, ownerID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

// ActiveUsageByOwner 返回某用户 active 文件的个数与总占用。
//
// 为什么两个值一次查：文件数与占用必须来自同一时刻的同一口径。
// 分成两次查询时，若中间有文件到期或删除，会出现"3 个文件 · 只统计了 2 个的大小"
// 这类自相矛盾的展示。口径与用户管理页 / 我的文件列表一致：只算 active，
// 回收站里的不计入。
func (s *Store) ActiveUsageByOwner(ctx context.Context, ownerID int64) (int64, int64, error) {
	var n, bytes int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), CAST(COALESCE(SUM(size_bytes), 0) AS SIGNED) FROM files
		 WHERE owner_id = ? AND status = ?`,
		ownerID, model.StatusActive).Scan(&n, &bytes)
	return n, bytes, err
}

// AllRelPaths 返回所有文件的相对路径（一致性扫描用）。
func (s *Store) AllRelPaths(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, rel_path FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		out[p] = id
	}
	return out, rows.Err()
}

// PermanentUsage 汇总**全站**永久文件占用的字节数。
//
// 口径见 settings.PermanentQuotaMB：只算 active 的永久文件。
// 回收站里的永久文件不算 —— 它们已设 purge_at、几天内必被清掉，
// 若算进来会让"删文件"这个唯一的自救手段不释放额度。
func (s *Store) PermanentUsage(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT CAST(COALESCE(SUM(size_bytes), 0) AS SIGNED) FROM files
		 WHERE status = ? AND expires_at IS NULL`,
		model.StatusActive).Scan(&n)
	return n, err
}

// SetExpiry 把某个文件改为永久（expiresAt=nil）或改为有期限。
//
// 只改 expires_at，**不动 status**：由服务层决定允许哪些状态。
// 返回受影响行数，0 表示文件不存在。
func (s *Store) SetExpiry(ctx context.Context, id int64, expiresAt *time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET expires_at = ? WHERE id = ?`,
		expiresArg(expiresAt), id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetExpiryUnderPath 把某目录树下（含子孙）的 active 文件批量设为永久或改为有期限。
//
// dirRel 是**磁盘相对路径前缀**，与 CountFilesUnderFolder 用同一套前缀匹配，
// 因此"递归范围"和"统计该目录下有多少文件"的口径天然一致 ——
// 不会出现"提示 10 个文件、实际只改了 8 个"。
func (s *Store) SetExpiryUnderPath(ctx context.Context, ownerID int64, dirRel string, expiresAt *time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE files SET expires_at = ?
		 WHERE owner_id = ? AND status = ? AND rel_path LIKE ?`,
		expiresArg(expiresAt), ownerID, model.StatusActive, escapeLike(dirRel)+"/%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PermanentizableStats 汇总"若把该目录树设为永久，会新增占用多少字节、涉及多少个文件"。
//
// 只算**当前还不是永久**的 active 文件（expires_at IS NOT NULL）：
// 已经是永久的那些早就计入配额了，再算一遍会让差额提示虚高，
// 用户看着"还差 3GB"却怎么清理都设不上。
//
// 同时返回文件数：配额不足的提示要写明"本次涉及 N 个文件"，
// 光有字节数只能瞎猜一个数字（曾写死成 1，200 个文件的目录也报"涉及 1 个文件"）。
func (s *Store) PermanentizableStats(ctx context.Context, ownerID int64, dirRel string) (int64, int64, error) {
	var count, n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), CAST(COALESCE(SUM(size_bytes), 0) AS SIGNED) FROM files
		 WHERE owner_id = ? AND status = ? AND expires_at IS NOT NULL AND rel_path LIKE ?`,
		ownerID, model.StatusActive, escapeLike(dirRel)+"/%").Scan(&count, &n)
	return count, n, err
}

// Stats 汇总管理端看板数据。
func (s *Store) Stats(ctx context.Context, now time.Time) (model.Stats, error) {
	var st model.Stats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM files WHERE status = ?),
			(SELECT COUNT(*) FROM files WHERE status = ?),
			(SELECT CAST(COALESCE(SUM(size_bytes),0) AS SIGNED) FROM files WHERE status = ?),
			(SELECT CAST(COALESCE(SUM(size_bytes),0) AS SIGNED) FROM files WHERE status = ?),
			(SELECT COUNT(*) FROM files WHERE status = ? AND expires_at <= ? AND expires_at > ?),
			(SELECT COUNT(*) FROM upload_sessions WHERE status = ?),
			(SELECT COUNT(*) FROM files WHERE status = ? AND purge_at IS NOT NULL AND purge_at <= ?)`,
		model.StatusActive, model.StatusTrashed,
		model.StatusActive, model.StatusTrashed,
		model.StatusActive, now.UTC().AddDate(0, 0, 7), now.UTC(),
		model.UploadUploading,
		model.StatusTrashed, now.UTC(),
	).Scan(&st.Users, &st.Files, &st.Trashed, &st.TotalBytes, &st.TrashedBytes,
		&st.Expiring7d, &st.UploadsInProgress, &st.ExpiredNotPurged)
	return st, err
}
