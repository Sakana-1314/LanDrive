package store

import (
	"context"
	"database/sql"
)

// GetSetting 读取单个配置项；不存在时返回 ok=false。
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT `value` FROM settings WHERE `key` = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// AllSettings 读取全部配置项。
func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT `key`, `value` FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// PutSettings 在单个事务中写入多个配置项。
func (s *Store) PutSettings(ctx context.Context, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, v := range kv {
		// 用显式参数而非 VALUES()：后者在 MySQL 8.0.20+ 已弃用。
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (`key`, `value`) VALUES (?, ?) "+
				"ON DUPLICATE KEY UPDATE `value` = ?", k, v, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
