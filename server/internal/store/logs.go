package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"lan-drive/internal/model"
)

// InsertLog 写一条审计日志。写日志失败不应影响主流程，调用方按需忽略错误。
func (s *Store) InsertLog(ctx context.Context, e *model.LogEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO op_logs (user_id, employee_no, action, target_type, target_id, detail, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.EmployeeNo, e.Action, e.TargetType, e.TargetID, truncate(e.Detail, 512), e.IP)
	return err
}

// LogQuery 审计日志检索条件。
type LogQuery struct {
	Action   string
	Keyword  string
	UserID   int64
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

func (q LogQuery) conditions() (string, []any) {
	conds := []string{"1=1"}
	args := []any{}
	if a := strings.TrimSpace(q.Action); a != "" {
		conds = append(conds, "action = ?")
		args = append(args, a)
	}
	if q.UserID > 0 {
		conds = append(conds, "user_id = ?")
		args = append(args, q.UserID)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		conds = append(conds, "(employee_no LIKE ? OR detail LIKE ? OR target_id LIKE ?)")
		args = append(args, like, like, like)
	}
	if q.From != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, q.From.UTC())
	}
	if q.To != nil {
		conds = append(conds, "created_at <= ?")
		args = append(args, q.To.UTC())
	}
	return strings.Join(conds, " AND "), args
}

// ListLogs 分页查询审计日志。
func (s *Store) ListLogs(ctx context.Context, q LogQuery) ([]model.LogEntry, int64, error) {
	where, args := q.conditions()

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM op_logs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, employee_no, action, target_type, target_id, detail, ip, created_at
		 FROM op_logs WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]model.LogEntry, 0, q.PageSize)
	for rows.Next() {
		var e model.LogEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &uid, &e.EmployeeNo, &e.Action, &e.TargetType,
			&e.TargetID, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		e.UserID = nullID(uid)
		e.CreatedAt = e.CreatedAt.UTC()
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// PurgeLogs 删除早于 before 的审计日志，返回删除条数。
func (s *Store) PurgeLogs(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM op_logs WHERE created_at < ?`, before.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListRecentLogs 返回最近的若干条日志（看板用）。
func (s *Store) ListRecentLogs(ctx context.Context, limit int) ([]model.LogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, employee_no, action, target_type, target_id, detail, ip, created_at
		 FROM op_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.LogEntry{}
	for rows.Next() {
		var e model.LogEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &uid, &e.EmployeeNo, &e.Action, &e.TargetType,
			&e.TargetID, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.UserID = nullID(uid)
		e.CreatedAt = e.CreatedAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// DistinctActions 返回日志中实际出现过的动作（前端筛选下拉）。
func (s *Store) DistinctActions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT action FROM op_logs ORDER BY action ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TryLock 尝试获取命名锁（多副本部署时保证维护任务只有一个实例执行）。
// 返回的 release 必须在任务结束后调用。
func (s *Store) TryLock(ctx context.Context, name string, timeoutSeconds int) (bool, func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, nil, err
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, ?)`, name, timeoutSeconds).Scan(&got); err != nil {
		_ = conn.Close()
		return false, nil, err
	}
	if !got.Valid || got.Int64 != 1 {
		_ = conn.Close()
		return false, nil, nil
	}
	release := func() {
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(rctx, `SELECT RELEASE_LOCK(?)`, name)
		_ = conn.Close()
	}
	return true, release, nil
}

// Ping 健康检查。
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// SchemaVersion 返回已应用的最高迁移版本（健康检查展示用）。
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, fmt.Errorf("尚无已应用的迁移")
	}
	return int(v.Int64), nil
}
