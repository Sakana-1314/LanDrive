package auth

import (
	"strings"
	"testing"
	"time"

	"lan-drive/internal/model"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("CorrectHorse123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "CorrectHorse123" {
		t.Fatalf("密码不应明文存储")
	}
	if !CheckPassword(hash, "CorrectHorse123") {
		t.Fatalf("正确密码应校验通过")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatalf("错误密码不应通过")
	}
	// bcrypt 每次加盐，哈希应不同
	hash2, _ := HashPassword("CorrectHorse123")
	if hash == hash2 {
		t.Fatalf("相同密码的哈希应因加盐而不同")
	}
	if !CheckPassword(hash2, "CorrectHorse123") {
		t.Fatalf("第二个哈希也应能校验")
	}
}

func TestHashPasswordRejectsBadLengths(t *testing.T) {
	if _, err := HashPassword("abc"); err == nil {
		t.Fatalf("过短密码应被拒绝")
	}
	if _, err := HashPassword(strings.Repeat("a", 200)); err == nil {
		t.Fatalf("过长密码应被拒绝")
	}
}

func TestTokenIssueAndParse(t *testing.T) {
	m := NewTokenManager(strings.Repeat("s", 32), time.Hour)
	token, exp, err := m.Issue(42, "1001", "admin", "$2a$10$abcdefghijklmnopqrstuv")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatalf("令牌不应为空")
	}
	if time.Until(exp) < 50*time.Minute {
		t.Fatalf("过期时间不合理: %v", exp)
	}
	claims, err := m.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != "42" || claims.EmployeeNo != "1001" || claims.Role != "admin" {
		t.Fatalf("载荷不正确: %+v", claims)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	m := NewTokenManager(strings.Repeat("s", 32), time.Hour)
	token, _, _ := m.Issue(1, "e", "user", "hash")

	// 篡改签名
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT 应为三段")
	}
	tampered := parts[0] + "." + parts[1] + ".AAAA" + parts[2][4:]
	if _, err := m.Parse(tampered); err == nil {
		t.Fatalf("被篡改的令牌不应通过")
	}

	// 换密钥后旧令牌失效
	m2 := NewTokenManager(strings.Repeat("x", 32), time.Hour)
	if _, err := m2.Parse(token); err == nil {
		t.Fatalf("不同密钥签发/解析不应通过")
	}

	// 垃圾字符串
	if _, err := m.Parse("not-a-token"); err == nil {
		t.Fatalf("非法令牌不应通过")
	}
}

func TestTokenExpiry(t *testing.T) {
	// TTL 为负会被规范化为默认值，这里用极短 TTL 验证过期。
	m := NewTokenManager(strings.Repeat("s", 32), time.Millisecond)
	token, _, _ := m.Issue(1, "e", "user", "hash")
	time.Sleep(1100 * time.Millisecond)
	// 注：jwt/v5 的过期校验按秒计，额外等一下确保跨秒。
	time.Sleep(500 * time.Millisecond)
	if _, err := m.Parse(token); err == nil {
		t.Fatalf("过期令牌不应通过")
	}
}

func TestPwdFingerprintInvalidatesTokenOnPasswordChange(t *testing.T) {
	oldHash, _ := HashPassword("old-password")
	newHash, _ := HashPassword("new-password")

	if !VerifyPwdVer(PwdFingerprint(oldHash), oldHash) {
		t.Fatalf("同一哈希应校验通过")
	}
	if VerifyPwdVer(PwdFingerprint(oldHash), newHash) {
		t.Fatalf("密码变更后旧令牌指纹应失效")
	}
	// 指纹不得泄露完整哈希
	if len(PwdFingerprint(oldHash)) != 16 {
		t.Fatalf("指纹长度应为 16，实际 %d", len(PwdFingerprint(oldHash)))
	}
}

func TestLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	key := "1.2.3.4|1001"

	for i := 0; i < 3; i++ {
		if !l.Allow(key) {
			t.Fatalf("第 %d 次尝试应被允许", i+1)
		}
		l.Fail(key)
	}
	if l.Allow(key) {
		t.Fatalf("超过上限后应被拒绝")
	}
	// 其它 key 不受影响
	if !l.Allow("5.6.7.8|1002") {
		t.Fatalf("其它来源不应被连带限制")
	}
	// 成功后重置
	l.Reset(key)
	if !l.Allow(key) {
		t.Fatalf("重置后应恢复")
	}
}

func TestLimiterWindowExpires(t *testing.T) {
	l := NewLimiter(1, 60*time.Millisecond)
	key := "k"
	l.Fail(key)
	if l.Allow(key) {
		t.Fatalf("窗口内应被拒绝")
	}
	time.Sleep(80 * time.Millisecond)
	if !l.Allow(key) {
		t.Fatalf("窗口过后应恢复")
	}
}

func TestLimiterSweep(t *testing.T) {
	l := NewLimiter(1, 40*time.Millisecond)
	l.Fail("a")
	l.Fail("b")
	time.Sleep(60 * time.Millisecond)
	l.Sweep()
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("Sweep 应清理过期计数，剩余 %d", n)
	}
}

func TestUserCache(t *testing.T) {
	c := NewUserCache[string](50 * time.Millisecond)
	if _, ok := c.Get(1); ok {
		t.Fatalf("空缓存不应命中")
	}
	c.Put(1, "alice")
	if v, ok := c.Get(1); !ok || v != "alice" {
		t.Fatalf("应命中缓存")
	}
	// 失效
	c.Invalidate(1)
	if _, ok := c.Get(1); ok {
		t.Fatalf("失效后不应命中（保证停用/改角色立即生效）")
	}
	// TTL 过期
	c.Put(2, "bob")
	time.Sleep(70 * time.Millisecond)
	if _, ok := c.Get(2); ok {
		t.Fatalf("超过 TTL 不应命中")
	}
	// Clear
	c.Put(3, "carol")
	c.Clear()
	if _, ok := c.Get(3); ok {
		t.Fatalf("Clear 后不应命中")
	}
}

// TestPublicCopyDoesNotAffectCache 是针对一个真实缺陷的回归测试：
// 登录处理器曾把「已放入 UserCache 的同一个指针」的密码原地清空，
// 导致缓存里的用户丢失密码哈希，后续所有请求的令牌指纹校验失败，
// 表现为创建用户后立刻提示「密码已变更，请重新登录」。
func TestPublicCopyDoesNotAffectCache(t *testing.T) {
	cache := NewUserCache[*model.User](time.Minute)
	u := &model.User{ID: 1, EmployeeNo: "1001", Name: "张三", Password: "bcrypt-hash-value"}

	// 模拟登录流程：放入缓存 → 外发副本。
	cache.Put(u.ID, u)
	sent := u.Public()
	sent.Password = "" // 即使调用方再次改写副本，也不能影响缓存

	// 缓存中的对象必须仍然保留完整密码哈希。
	cached, ok := cache.Get(u.ID)
	if !ok {
		t.Fatalf("缓存应命中")
	}
	if cached.Password != "bcrypt-hash-value" {
		t.Fatalf("缓存中的密码哈希被污染: %q", cached.Password)
	}
	// 指纹校验必须持续有效（否则用户会被莫名强制重新登录）。
	if !VerifyPwdVer(PwdFingerprint(cached.Password), cached.Password) {
		t.Fatalf("缓存用户应能通过令牌指纹校验")
	}
	// 外发副本不得包含密码哈希
	if sent.Password != "" {
		t.Fatalf("外发副本不应包含密码哈希")
	}
	// 副本是独立对象，改副本不能影响原对象
	if u.Password != "bcrypt-hash-value" {
		t.Fatalf("Public 必须返回副本而非原地修改")
	}
}
