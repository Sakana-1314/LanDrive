package store

import "context"

// PinOwner 置顶某个人的人员目录（对 viewerID 自己生效）。重复置顶是幂等的。
func (s *Store) PinOwner(ctx context.Context, viewerID, targetID int64) error {
	// INSERT IGNORE：重复点击置顶不应报错，前端可能因网络重试而重发。
	_, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO user_pins (owner_user_id, target_user_id) VALUES (?, ?)`,
		viewerID, targetID)
	return err
}

// UnpinOwner 取消置顶。未置顶时删除 0 行，同样视为成功（幂等）。
func (s *Store) UnpinOwner(ctx context.Context, viewerID, targetID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_pins WHERE owner_user_id = ? AND target_user_id = ?`,
		viewerID, targetID)
	return err
}
