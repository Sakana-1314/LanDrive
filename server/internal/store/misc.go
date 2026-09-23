package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

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
