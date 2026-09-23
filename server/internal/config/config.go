// Package config 通过环境变量加载配置。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 运行配置。
type Config struct {
	Listen        string   // LANDRIVE_LISTEN 默认 :8080
	MySQLDSN      string   // LANDRIVE_MYSQL_DSN 必填
	AutoCreateDB  bool     // LANDRIVE_AUTO_CREATE_DB 默认 true
	JWTSecret     string   // LANDRIVE_JWT_SECRET 必填，>=32 字符
	DataDir       string   // LANDRIVE_DATA_DIR 默认 ./data
	AdminEmpNo    string   // LANDRIVE_ADMIN_EMPLOYEE_NO 默认 admin
	AdminPassword string   // LANDRIVE_ADMIN_PASSWORD 首次启动种子密码
	AdminName     string   // LANDRIVE_ADMIN_NAME 默认 系统管理员
	TrustProxy    bool     // LANDRIVE_TRUST_PROXY 默认 false
	CORSAllow     []string // LANDRIVE_CORS_ALLOW 逗号分隔
	LogKeepDays   int      // LANDRIVE_LOG_KEEP_DAYS 默认 90
}

// Load 读取环境变量并校验。
func Load() (*Config, error) {
	c := &Config{
		Listen:        envStr("LANDRIVE_LISTEN", ":8080"),
		MySQLDSN:      envStr("LANDRIVE_MYSQL_DSN", ""),
		AutoCreateDB:  envBool("LANDRIVE_AUTO_CREATE_DB", true),
		JWTSecret:     envStr("LANDRIVE_JWT_SECRET", ""),
		DataDir:       envStr("LANDRIVE_DATA_DIR", "./data"),
		AdminEmpNo:    envStr("LANDRIVE_ADMIN_EMPLOYEE_NO", "admin"),
		AdminPassword: envStr("LANDRIVE_ADMIN_PASSWORD", ""),
		AdminName:     envStr("LANDRIVE_ADMIN_NAME", "系统管理员"),
		TrustProxy:    envBool("LANDRIVE_TRUST_PROXY", false),
		CORSAllow:     envList("LANDRIVE_CORS_ALLOW", nil),
		LogKeepDays:   envInt("LANDRIVE_LOG_KEEP_DAYS", 90),
	}

	var errs []string
	if c.MySQLDSN == "" {
		errs = append(errs, "LANDRIVE_MYSQL_DSN 必填，示例：lanfs:pass@tcp(127.0.0.1:3306)/lanfs")
	}
	if c.JWTSecret == "" {
		errs = append(errs, "LANDRIVE_JWT_SECRET 必填，长度至少 32 字符")
	} else if len(c.JWTSecret) < 32 {
		errs = append(errs, "LANDRIVE_JWT_SECRET 长度至少 32 字符")
	}
	if c.DataDir == "" {
		errs = append(errs, "LANDRIVE_DATA_DIR 不能为空")
	}
	if strings.TrimSpace(c.AdminEmpNo) == "" {
		errs = append(errs, "LANDRIVE_ADMIN_EMPLOYEE_NO 不能为空")
	}
	if c.LogKeepDays < 1 {
		c.LogKeepDays = 90
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "；"))
	}
	return c, nil
}

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envList(key string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// String 输出可安全打印的配置摘要（不含密钥）。
func (c *Config) String() string {
	return fmt.Sprintf("listen=%s data_dir=%s auto_create_db=%v admin=%s trust_proxy=%v",
		c.Listen, c.DataDir, c.AutoCreateDB, c.AdminEmpNo, c.TrustProxy)
}
