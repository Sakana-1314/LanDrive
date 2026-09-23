package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"lan-drive/internal/model"
)

const userCols = `u.id, u.employee_no, u.name, u.password_hash, u.role, u.enabled, u.dir_rel,
	u.last_login_at, u.created_at, u.updated_at`

func scanUser(sc interface {
	Scan(dest ...any) error
}) (*model.User, error) {
	var u model.User
	var enabled int
	var lastLogin sql.NullTime
	if err := sc.Scan(&u.ID, &u.EmployeeNo, &u.Name, &u.Password, &u.Role, &enabled, &u.DirRel,
		&lastLogin, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.Enabled = enabled == 1
	u.LastLoginAt = nullTime(lastLogin)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return &u, nil
}

// CreateUser 新建账号，返回带 ID 的实体（调用方据此创建目录）。
func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	enabled := 0
	if u.Enabled {
		enabled = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (employee_no, name, password_hash, role, enabled, dir_rel)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		u.EmployeeNo, u.Name, u.Password, u.Role, enabled, u.DirRel)
	if err != nil {
		if isDuplicate(err) {
			return fmt.Errorf("%w: 工号 %s 已存在", ErrConflict, u.EmployeeNo)
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

// GetUserByEmployeeNo 按工号查询（含停用账号）。
func (s *Store) GetUserByEmployeeNo(ctx context.Context, employeeNo string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.employee_no = ?`, employeeNo)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

// GetUserByID 按主键查询。
func (s *Store) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id = ?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

// CountUsers 返回账号总数。
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountAdmins 返回启用状态的管理员数量（用于阻止删除最后一个管理员）。
func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = ? AND enabled = 1`, model.RoleAdmin).Scan(&n)
	return n, err
}

// ListUsers 分页查询账号，附带 active 文件数与占用字节。
// q 同时匹配工号与姓名。
func (s *Store) ListUsers(ctx context.Context, q string, page, pageSize int) ([]model.User, int64, error) {
	where := "WHERE 1=1"
	args := []any{}
	if q = strings.TrimSpace(q); q != "" {
		where += " AND (u.employee_no LIKE ? OR u.name LIKE ?)"
		like := "%" + escapeLike(q) + "%"
		args = append(args, like, like)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sqlText := `SELECT ` + userCols + `,
		COALESCE(f.cnt, 0) AS file_count, COALESCE(f.bytes, 0) AS used_bytes
		FROM users u
		LEFT JOIN (
			SELECT owner_id, COUNT(*) AS cnt, COALESCE(SUM(size_bytes), 0) AS bytes
			FROM files WHERE status = ? GROUP BY owner_id
		) f ON f.owner_id = u.id
		` + where + ` ORDER BY u.id ASC LIMIT ? OFFSET ?`
	args = append([]any{model.StatusActive}, args...)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]model.User, 0, pageSize)
	for rows.Next() {
		var u model.User
		var enabled int
		var lastLogin sql.NullTime
		if err := rows.Scan(&u.ID, &u.EmployeeNo, &u.Name, &u.Password, &u.Role, &enabled, &u.DirRel,
			&lastLogin, &u.CreatedAt, &u.UpdatedAt, &u.FileCount, &u.UsedBytes); err != nil {
			return nil, 0, err
		}
		u.Enabled = enabled == 1
		u.LastLoginAt = nullTime(lastLogin)
		u.CreatedAt = u.CreatedAt.UTC()
		u.UpdatedAt = u.UpdatedAt.UTC()
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// UserUpdate 描述可更新的账号字段，nil 表示不改。
type UserUpdate struct {
	Name     *string
	Role     *string
	Enabled  *bool
	Password *string // bcrypt 哈希
}

// UpdateUser 局部更新账号。
func (s *Store) UpdateUser(ctx context.Context, id int64, up UserUpdate) error {
	sets := []string{}
	args := []any{}
	if up.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *up.Name)
	}
	if up.Role != nil {
		sets = append(sets, "role = ?")
		args = append(args, *up.Role)
	}
	if up.Enabled != nil {
		v := 0
		if *up.Enabled {
			v = 1
		}
		sets = append(sets, "enabled = ?")
		args = append(args, v)
	}
	if up.Password != nil {
		sets = append(sets, "password_hash = ?")
		args = append(args, *up.Password)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// ClientFoundRows=true 时 matched 行数为 0 才代表不存在。
		if _, gerr := s.GetUserByID(ctx, id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// DeleteUser 删除账号；调用方需保证其文件已被处理（外键 RESTRICT 兜底）。
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		// 外键 RESTRICT 触发时 MySQL 返回 1451（ER_ROW_IS_REFERENCED_2）。
		// 不能只靠英文错误文案（大小写/驱动差异会导致漏判）。
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: 该用户仍有文件记录", ErrInUse)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserDirRel 写入用户目录的相对路径。
// 目录名依赖自增主键，因此必须在插入拿到 ID 之后回填。
func (s *Store) SetUserDirRel(ctx context.Context, id int64, dirRel string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET dir_rel = ? WHERE id = ?`, dirRel, id)
	return err
}

// TouchLastLogin 记录最后登录时间。
func (s *Store) TouchLastLogin(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, nowUTC(), id)
	return err
}

// ListOwners 按用户目录聚合 active 文件（用于前端的用户目录树）。
// 没有文件的账号也会返回，便于展示空目录。
//
// viewerID 用于标记"当前登录用户是否置顶了此人"，并据此排序：
// 置顶项排在前面，其余按 id 升序。排序放在服务端做，前端只负责渲染，
// 避免各端各自排序导致顺序不一致。
func (s *Store) ListOwners(ctx context.Context, viewerID int64) ([]model.OwnerAggregate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.employee_no, u.name, u.dir_rel,
			COALESCE(f.cnt, 0), COALESCE(f.bytes, 0),
			(p.owner_user_id IS NOT NULL) AS pinned
		 FROM users u
		 LEFT JOIN (
			SELECT owner_id, COUNT(*) AS cnt, COALESCE(SUM(size_bytes), 0) AS bytes
			FROM files WHERE status = ? GROUP BY owner_id
		 ) f ON f.owner_id = u.id
		 LEFT JOIN user_pins p ON p.target_user_id = u.id AND p.owner_user_id = ?
		 ORDER BY pinned DESC, u.id ASC`, model.StatusActive, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.OwnerAggregate{}
	for rows.Next() {
		var a model.OwnerAggregate
		var pinned int
		if err := rows.Scan(&a.UserID, &a.EmployeeNo, &a.Name, &a.DirRel,
			&a.FileCount, &a.UsedBytes, &pinned); err != nil {
			return nil, err
		}
		a.Pinned = pinned == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// escapeLike 转义 LIKE 通配符，避免用户输入的 % 与 _ 破坏查询语义。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
