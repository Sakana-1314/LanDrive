package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadRequiresEssentialFields(t *testing.T) {
	setEnv(t, map[string]string{
		"LANDRIVE_MYSQL_DSN":  "",
		"LANDRIVE_JWT_SECRET": "",
	})
	if _, err := Load(); err == nil {
		t.Fatalf("缺少必填项时应报错")
	}

	// 只填 DSN
	setEnv(t, map[string]string{"LANDRIVE_MYSQL_DSN": "u:p@tcp(127.0.0.1:3306)/db"})
	if _, err := Load(); err == nil {
		t.Fatalf("缺少 JWT 密钥时应报错")
	}

	// JWT 密钥过短
	setEnv(t, map[string]string{
		"LANDRIVE_MYSQL_DSN":  "u:p@tcp(127.0.0.1:3306)/db",
		"LANDRIVE_JWT_SECRET": "short",
	})
	if _, err := Load(); err == nil {
		t.Fatalf("JWT 密钥过短时应报错")
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{
		"LANDRIVE_MYSQL_DSN":  "u:p@tcp(127.0.0.1:3306)/db",
		"LANDRIVE_JWT_SECRET": strings.Repeat("s", 32),
	})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != ":8080" {
		t.Fatalf("默认监听地址 = %q", c.Listen)
	}
	if c.DataDir != "./data" {
		t.Fatalf("默认数据目录 = %q", c.DataDir)
	}
	if !c.AutoCreateDB {
		t.Fatalf("默认应自动建库")
	}
	if c.TrustProxy {
		t.Fatalf("默认不应信任代理")
	}
	if c.AdminEmpNo != "admin" {
		t.Fatalf("默认管理员工号 = %q", c.AdminEmpNo)
	}
	if c.AdminName != "系统管理员" {
		t.Fatalf("默认管理员姓名 = %q", c.AdminName)
	}
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"LANDRIVE_MYSQL_DSN":         "u:p@tcp(db:3306)/lanfs",
		"LANDRIVE_JWT_SECRET":        strings.Repeat("k", 40),
		"LANDRIVE_LISTEN":            ":9000",
		"LANDRIVE_DATA_DIR":          "/data",
		"LANDRIVE_AUTO_CREATE_DB":    "false",
		"LANDRIVE_ADMIN_EMPLOYEE_NO": "root",
		"LANDRIVE_ADMIN_PASSWORD":    "secret-pass",
		"LANDRIVE_ADMIN_NAME":        "管理员甲",
		"LANDRIVE_TRUST_PROXY":       "true",
		"LANDRIVE_CORS_ALLOW":        "http://localhost:5173, http://127.0.0.1:5173",
	})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != ":9000" || c.DataDir != "/data" || c.AutoCreateDB {
		t.Fatalf("配置覆盖未生效: %+v", c)
	}
	if c.AdminEmpNo != "root" || c.AdminName != "管理员甲" || c.AdminPassword != "secret-pass" {
		t.Fatalf("管理员配置未生效: %+v", c)
	}
	if !c.TrustProxy {
		t.Fatalf("信任代理开关未生效")
	}
	if len(c.CORSAllow) != 2 || c.CORSAllow[0] != "http://localhost:5173" {
		t.Fatalf("CORS 白名单解析错误: %v", c.CORSAllow)
	}
}

func TestLoadInvalidBoolFallsBackToDefault(t *testing.T) {
	setEnv(t, map[string]string{
		"LANDRIVE_MYSQL_DSN":      "u:p@tcp(127.0.0.1:3306)/db",
		"LANDRIVE_JWT_SECRET":     strings.Repeat("s", 32),
		"LANDRIVE_TRUST_PROXY":    "not-a-bool",
		"LANDRIVE_AUTO_CREATE_DB": "also-not-a-bool",
	})
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.TrustProxy {
		t.Fatalf("非法布尔值应回退默认 false")
	}
	if !c.AutoCreateDB {
		t.Fatalf("非法布尔值应回退默认 true")
	}
}

func TestStringHidesSecrets(t *testing.T) {
	c := &Config{
		Listen:       ":8080",
		MySQLDSN:     "u:secret@tcp(127.0.0.1:3306)/db",
		JWTSecret:    "super-secret-key-value",
		DataDir:      "./data",
		AutoCreateDB: true,
		AdminEmpNo:   "admin",
	}
	s := c.String()
	if strings.Contains(s, "secret") {
		t.Fatalf("配置摘要不应包含任何密钥: %q", s)
	}
	if !strings.Contains(s, ":8080") {
		t.Fatalf("配置摘要应包含监听地址: %q", s)
	}
}
