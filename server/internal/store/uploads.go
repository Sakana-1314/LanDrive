package store

import (
	"context"
	"database/sql"
	"time"

	"lan-drive/internal/model"
)

const uploadCols = `s.id, s.owner_id, s.original_name, s.ext, s.size_bytes, s.chunk_size,
	s.total_chunks, s.received_bytes, s.status, s.dir_rel, s.folder_id, s.sha256, s.file_id,
	s.created_at, s.updated_at`

func scanUpload(sc interface {
	Scan(dest ...any) error
}) (*model.UploadSession, error) {
	var u model.UploadSession
	var fileID sql.NullInt64
	if err := sc.Scan(&u.ID, &u.OwnerID, &u.OriginalName, &u.Ext, &u.SizeBytes, &u.ChunkSize,
		&u.TotalChunks, &u.ReceivedBytes, &u.Status, &u.DirRel, &u.FolderID, &u.SHA256, &fileID,
		&u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.FileID = nullID(fileID)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return &u, nil
}

// CreateUploadSession 创建一个新的分片上传会话。
func (s *Store) CreateUploadSession(ctx context.Context, u *model.UploadSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO upload_sessions (id, owner_id, original_name, ext, size_bytes, chunk_size,
			total_chunks, received_bytes, status, dir_rel, folder_id, sha256)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		u.ID, u.OwnerID, u.OriginalName, u.Ext, u.SizeBytes, u.ChunkSize,
		u.TotalChunks, model.UploadUploading, u.DirRel, u.FolderID, u.SHA256)
	if err != nil && isDuplicate(err) {
		return ErrConflict
	}
	return err
}

// GetUploadSession 按 ID 查询会话。
func (s *Store) GetUploadSession(ctx context.Context, id string) (*model.UploadSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+uploadCols+` FROM upload_sessions s WHERE s.id = ?`, id)
	u, err := scanUpload(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

// FindOpenSession 查找可复用的未完成会话：同属主 + 同名 + 同大小 + 同 sha。
// sha 为空时只按前三者匹配（客户端未提供校验值的情况）。
func (s *Store) FindOpenSession(ctx context.Context, ownerID int64, name string, size int64, sha string) (*model.UploadSession, error) {
	q := `SELECT ` + uploadCols + ` FROM upload_sessions s
		WHERE s.owner_id = ? AND s.original_name = ? AND s.size_bytes = ? AND s.status = ?`
	args := []any{ownerID, name, size, model.UploadUploading}
	if sha != "" {
		q += ` AND s.sha256 = ?`
		args = append(args, sha)
	} else {
		q += ` AND s.sha256 = ''`
	}
	q += ` ORDER BY s.id ASC LIMIT 1`

	u, err := scanUpload(s.db.QueryRowContext(ctx, q, args...))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

// ListChunks 返回会话已落盘的分片（按序号升序）。
func (s *Store) ListChunks(ctx context.Context, uploadID string) ([]model.Chunk, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT idx, size_bytes, rel_path FROM upload_chunks WHERE upload_id = ? ORDER BY idx ASC`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Chunk{}
	for rows.Next() {
		var c model.Chunk
		if err := rows.Scan(&c.Idx, &c.SizeBytes, &c.RelPath); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChunk 查询单个分片是否已存在。
func (s *Store) GetChunk(ctx context.Context, uploadID string, idx int) (*model.Chunk, error) {
	var c model.Chunk
	err := s.db.QueryRowContext(ctx,
		`SELECT idx, size_bytes, rel_path FROM upload_chunks WHERE upload_id = ? AND idx = ?`,
		uploadID, idx).Scan(&c.Idx, &c.SizeBytes, &c.RelPath)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertChunk 记录一个分片（幂等覆盖：同一 idx 重复上传时更新大小），
// 并同步会话的已接收字节。
func (s *Store) UpsertChunk(ctx context.Context, uploadID string, idx int, size int64, relPath string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var oldSize sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT size_bytes FROM upload_chunks WHERE upload_id = ? AND idx = ?`, uploadID, idx).Scan(&oldSize)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO upload_chunks (upload_id, idx, size_bytes, rel_path) VALUES (?, ?, ?, ?)`,
			uploadID, idx, size, relPath); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE upload_sessions SET received_bytes = received_bytes + ? WHERE id = ?`,
			size, uploadID); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		delta := size - oldSize.Int64
		if _, err := tx.ExecContext(ctx,
			`UPDATE upload_chunks SET size_bytes = ?, rel_path = ? WHERE upload_id = ? AND idx = ?`,
			size, relPath, uploadID, idx); err != nil {
			return err
		}
		if delta != 0 {
			// received_bytes 为无符号列，重传更小的分片时按差值回退，最小到 0。
			if delta < 0 {
				if _, err := tx.ExecContext(ctx,
					`UPDATE upload_sessions SET received_bytes = IF(received_bytes > ?, received_bytes - ?, 0) WHERE id = ?`,
					-delta, -delta, uploadID); err != nil {
					return err
				}
			} else {
				if _, err := tx.ExecContext(ctx,
					`UPDATE upload_sessions SET received_bytes = received_bytes + ? WHERE id = ?`,
					delta, uploadID); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// RecomputeReceived 按分片表重算 received_bytes（用于自愈）。
func (s *Store) RecomputeReceived(ctx context.Context, uploadID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE upload_sessions SET received_bytes =
			(SELECT CAST(COALESCE(SUM(size_bytes),0) AS SIGNED) FROM upload_chunks WHERE upload_id = ?)
		 WHERE id = ?`, uploadID, uploadID)
	return err
}

// FinishUploadSession 把会话置为 done 并记录产物文件 ID
// （CAS：仅 uploading → done 才算成功）。
// 返回 true 表示本次调用完成了状态迁移（即首次完成）。
func (s *Store) FinishUploadSession(ctx context.Context, uploadID string, fileID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE upload_sessions SET status = ?, file_id = ? WHERE id = ? AND status = ?`,
		model.UploadDone, fileID, uploadID, model.UploadUploading)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteChunks 删除会话的全部分片记录（合并完成后清理，磁盘分片由调用方删除）。
func (s *Store) DeleteChunks(ctx context.Context, uploadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_chunks WHERE upload_id = ?`, uploadID)
	return err
}

// ListFinishedSessions 返回已结束（done / aborted）且超过 before 未更新的会话，
// 供维护任务回收（保留一段时间以便 complete 幂等返回与排障）。
func (s *Store) ListFinishedSessions(ctx context.Context, before time.Time, limit int) ([]model.UploadSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+uploadCols+` FROM upload_sessions s
		 WHERE s.status <> ? AND s.updated_at < ? ORDER BY s.id ASC LIMIT ?`,
		model.UploadUploading, before.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UploadSession
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// AbortUploadSession 标记会话为 aborted 并删除分片记录。
func (s *Store) AbortUploadSession(ctx context.Context, uploadID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM upload_chunks WHERE upload_id = ?`, uploadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE upload_sessions SET status = ?, received_bytes = 0 WHERE id = ?`,
		model.UploadAborted, uploadID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUploadSession 彻底删除会话（分片记录由外键级联删除）。
func (s *Store) DeleteUploadSession(ctx context.Context, uploadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id = ?`, uploadID)
	return err
}

// ListStaleUploads 返回超过 idle 未更新的未完成会话（僵尸会话清理）。
func (s *Store) ListStaleUploads(ctx context.Context, before time.Time, limit int) ([]model.UploadSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+uploadCols+` FROM upload_sessions s
		 WHERE s.status = ? AND s.updated_at < ? ORDER BY s.id ASC LIMIT ?`,
		model.UploadUploading, before.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UploadSession
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ListUploadSessionsByOwner 返回某用户指定状态的会话（删除账号前清理用）。
func (s *Store) ListUploadSessionsByOwner(ctx context.Context, ownerID int64, status string) ([]model.UploadSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+uploadCols+` FROM upload_sessions s WHERE s.owner_id = ? AND s.status = ? ORDER BY s.id ASC`,
		ownerID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UploadSession
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// AllChunkRelPaths 返回全部上传分片的相对路径集合（一致性扫描用）。
// 分片文件由 upload_chunks 表管理，不属于 files 表，因此扫描时必须排除，
// 否则会把正在进行的上传误报为孤儿文件。
func (s *Store) AllChunkRelPaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rel_path FROM upload_chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = struct{}{}
	}
	return out, rows.Err()
}
