// Package store 封装 MySQL 访问：连接、嵌入式迁移、仓储方法。
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	gomysql "github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// 领域错误。handler 层据此映射 HTTP 状态码。
var (
	ErrNotFound     = errors.New("记录不存在")
	ErrConflict     = errors.New("记录已存在")
	ErrInvalidInput = errors.New("输入不合法")
	ErrInUse        = errors.New("仍被引用，无法删除")
	ErrState        = errors.New("当前状态不允许该操作")
	ErrQuota        = errors.New("超出体积限制")
)

// Store 数据库句柄。
type Store struct {
	db *sql.DB
}

// DB 暴露底层连接（仅供维护任务与集成测试使用）。
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭连接。
func (s *Store) Close() error { return s.db.Close() }

// Open 建立连接（带重试，适配容器启动顺序），自动补全 DSN 参数、按需建库并执行迁移。
func Open(ctx context.Context, dsn string, autoCreate bool) (*Store, error) {
	parsed, err := gomysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 LANDRIVE_MYSQL_DSN 失败: %w", err)
	}
	if parsed.Params == nil {
		parsed.Params = map[string]string{}
	}
	if _, ok := parsed.Params["parseTime"]; !ok {
		parsed.Params["parseTime"] = "true"
	}
	if _, ok := parsed.Params["charset"]; !ok {
		parsed.Params["charset"] = "utf8mb4"
	}
	if _, ok := parsed.Params["collation"]; !ok {
		parsed.Params["collation"] = "utf8mb4_unicode_ci"
	}
	if _, ok := parsed.Params["time_zone"]; !ok {
		// 会话时区固定 UTC，保证 DATETIME 列全链路一致。
		parsed.Params["time_zone"] = "'+00:00'"
	}
	if parsed.Timeout == 0 {
		parsed.Timeout = 10 * time.Second
	}
	parsed.Loc = time.UTC
	// MySQL 默认返回“实际变更行数”，同值更新返回 0，会干扰幂等判断；统一取匹配行数。
	parsed.ClientFoundRows = true

	if autoCreate && parsed.DBName != "" {
		if err := ensureDatabase(ctx, parsed); err != nil {
			return nil, err
		}
	}

	final := parsed.FormatDSN()
	var db *sql.DB
	deadline := time.Now().Add(60 * time.Second)
	for {
		db, err = sql.Open("mysql", final)
		if err == nil {
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = db.PingContext(pctx)
			cancel()
			if err == nil {
				break
			}
			_ = db.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("连接 MySQL 失败（已重试 60s，请检查 LANDRIVE_MYSQL_DSN 与数据库是否就绪）: %w", err)
		}
		slog.Warn("MySQL 尚未就绪，2s 后重试", "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	db.SetMaxOpenConns(24)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// ensureDatabase 连接到实例（不带库名）并 CREATE DATABASE IF NOT EXISTS。
func ensureDatabase(ctx context.Context, cfg *gomysql.Config) error {
	noDB := *cfg
	noDB.DBName = ""
	conn, err := sql.Open("mysql", noDB.FormatDSN())
	if err != nil {
		return err
	}
	defer conn.Close()
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.PingContext(pctx); err != nil {
		return fmt.Errorf("连接 MySQL 实例失败: %w", err)
	}
	stmt := fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		strings.ReplaceAll(cfg.DBName, "`", ""))
	if _, err := conn.ExecContext(pctx, stmt); err != nil {
		return fmt.Errorf("创建数据库 %s 失败: %w", cfg.DBName, err)
	}
	return nil
}

var migrationFileRe = regexp.MustCompile(`^(\d+)_`)

// migrate 按文件名前缀版本号顺序执行未应用的迁移。
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT NOT NULL PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}

	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	type mig struct {
		version int
		name    string
	}
	var migs []mig
	for _, e := range entries {
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		migs = append(migs, mig{version: v, name: e.Name()})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(content)) {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("执行迁移 %s 失败: %w\nSQL: %s", m.name, err, truncate(stmt, 300))
			}
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, m.version); err != nil {
			return err
		}
		slog.Info("已应用数据库迁移", "file", m.name, "version", m.version)
	}
	return nil
}

// splitStatements 去掉注释行后按 ';' 切分 DDL。
// 本项目的迁移文件只包含建表语句，不含存储过程，因此这个简化实现足够。
func splitStatements(sqlText string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	var out []string
	for _, part := range strings.Split(b.String(), ";") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// isForeignKeyViolation 判断是否为主外键约束冲突。
//
// MySQL 相关错误码：1216/1217（通用外键错误）、1451（被引用无法删除）、
// 1452（引用了不存在的父行）。不同驱动返回的文案不一致，因此以错误码为准。
func isForeignKeyViolation(err error) bool {
	var me *gomysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1216, 1217, 1451, 1452:
			return true
		}
		// 兜底：少数驱动/兼容层只给文案。
		if strings.Contains(strings.ToLower(me.Message), "foreign key") {
			return true
		}
		return false
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "foreign key")
}

// isDuplicate 判断唯一键冲突。
func isDuplicate(err error) bool {
	var me *gomysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return false
}

// nowUTC 返回秒级精度的 UTC 时间（DATETIME 列为秒级）。
func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Second) }

// nullTime 把 sql.NullTime 转为 *time.Time。
func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

// nullID 把 sql.NullInt64 转为 *int64。
func nullID(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
