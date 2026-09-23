// Package auth 提供密码哈希、JWT 令牌与登录限流。
package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// BcryptCost 密码哈希强度。
const BcryptCost = 10

// TokenTTL 令牌默认有效期。
const TokenTTL = 12 * time.Hour

// 认证相关错误。
var (
	ErrBadCredentials = errors.New("工号或密码错误")
	ErrTokenInvalid   = errors.New("登录状态无效或已过期")
	ErrTooMany        = errors.New("尝试过于频繁，请稍后再试")
	ErrUserDisabled   = errors.New("账号已被停用，请联系管理员")
)

// HashPassword 生成 bcrypt 哈希。
func HashPassword(plain string) (string, error) {
	if len(plain) < 4 {
		return "", fmt.Errorf("密码长度至少 4 位")
	}
	if len(plain) > 128 {
		return "", fmt.Errorf("密码长度不能超过 128 位")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword 校验明文与哈希是否匹配。
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Claims 是签发的 JWT 载荷。
//
// PwdVer 是密码哈希的指纹：重置密码后旧令牌立即失效。
type Claims struct {
	EmployeeNo string `json:"emp"`
	Role       string `json:"role"`
	PwdVer     string `json:"pv"`
	jwt.RegisteredClaims
}

// TokenManager 负责签发与解析令牌。
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager 构造令牌管理器。
func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	if ttl <= 0 {
		ttl = TokenTTL
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

// TTL 返回令牌有效期。
func (m *TokenManager) TTL() time.Duration { return m.ttl }

// Issue 为指定用户签发令牌。
func (m *TokenManager) Issue(userID int64, employeeNo, role, passwordHash string) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(m.ttl)
	claims := Claims{
		EmployeeNo: employeeNo,
		Role:       role,
		PwdVer:     PwdFingerprint(passwordHash),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return s, exp, nil
}

// Parse 解析并校验令牌。
func (m *TokenManager) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期的签名算法: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	if claims.Subject == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// PwdFingerprint 计算密码哈希的短指纹，用于令牌失效判定。
// 只取哈希的前 16 个字符即可，不泄露完整哈希。
func PwdFingerprint(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16]
}

// VerifyPwdVer 恒定时间比较令牌中的密码指纹。
func VerifyPwdVer(claimVer, passwordHash string) bool {
	return subtle.ConstantTimeCompare([]byte(claimVer), []byte(PwdFingerprint(passwordHash))) == 1
}

// Limiter 是按 key（IP + 工号）计数的登录失败限流器。
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
}

type bucket struct {
	count int
	until time.Time
}

// NewLimiter 构造限流器：window 内最多 max 次失败。
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, hits: map[string]*bucket{}}
}

// Allow 报告该 key 当前是否允许尝试。
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.hits[key]
	if !ok {
		return true
	}
	if time.Now().After(b.until) {
		delete(l.hits, key)
		return true
	}
	return b.count < l.max
}

// Fail 记录一次失败。
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.hits[key]
	if !ok || now.After(b.until) {
		l.hits[key] = &bucket{count: 1, until: now.Add(l.window)}
		return
	}
	b.count++
}

// Reset 在成功登录后清除该 key 的失败计数。
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// Sweep 清理过期计数（由维护任务定期调用，避免 map 无限增长）。
func (l *Limiter) Sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, b := range l.hits {
		if now.After(b.until) {
			delete(l.hits, k)
		}
	}
}

// UserCache 是「鉴权后回查用户」的短 TTL 缓存。
// 停用/删除账号需要立即生效，因此 TTL 取 15 秒，并支持显式失效。
type UserCache[T any] struct {
	mu  sync.RWMutex
	ttl time.Duration
	m   map[int64]entry[T]
}

type entry[T any] struct {
	val T
	at  time.Time
}

// NewUserCache 构造缓存。
func NewUserCache[T any](ttl time.Duration) *UserCache[T] {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	return &UserCache[T]{ttl: ttl, m: map[int64]entry[T]{}}
}

// Get 读取缓存。
func (c *UserCache[T]) Get(id int64) (T, bool) {
	c.mu.RLock()
	e, ok := c.m[id]
	c.mu.RUnlock()
	var zero T
	if !ok || time.Since(e.at) > c.ttl {
		return zero, false
	}
	return e.val, true
}

// Put 写入缓存。
func (c *UserCache[T]) Put(id int64, v T) {
	c.mu.Lock()
	c.m[id] = entry[T]{val: v, at: time.Now()}
	c.mu.Unlock()
}

// Invalidate 使某个用户缓存失效（改角色/停用/重置密码后调用）。
func (c *UserCache[T]) Invalidate(id int64) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}

// Clear 清空缓存。
func (c *UserCache[T]) Clear() {
	c.mu.Lock()
	c.m = map[int64]entry[T]{}
	c.mu.Unlock()
}
