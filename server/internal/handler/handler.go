// Package handler 实现全部 HTTP 接口。
//
// 约定（与 docs/design.md 第 5 节一致）：
//   - 统一前缀 /api，错误响应恒为 {"error":"中文消息"}；
//   - 鉴权走 Authorization: Bearer <JWT>，鉴权后回查用户（带短 TTL 缓存）；
//   - 权限判定集中在 files 服务与 handlers 内，绝不信任前端传入的属主/角色。
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/auth"
	"lan-drive/internal/files"
	"lan-drive/internal/folders"
	"lan-drive/internal/maintain"
	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/shares"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
	"lan-drive/internal/upload"
)

// ctxUser 是 gin.Context 中保存当前登录用户的键。
const ctxUserKey = "lanfs.user"

// Handler 持有全部依赖。
type Handler struct {
	store   *store.Store
	st      *storage.Storage
	set     *settings.Service
	files   *files.Service
	folders *folders.Service
	shares  *shares.Service
	uploads *upload.Service
	maint   *maintain.Service
	tokens  *auth.TokenManager
	limiter *auth.Limiter
	users   *auth.UserCache[*model.User]
	started time.Time
}

// Deps 是构造 Handler 所需的依赖。
type Deps struct {
	Store    *store.Store
	Storage  *storage.Storage
	Settings *settings.Service
	Files    *files.Service
	Folders  *folders.Service
	Shares   *shares.Service
	Uploads  *upload.Service
	Maintain *maintain.Service
	Tokens   *auth.TokenManager
}

// New 构造 Handler。
func New(d Deps) *Handler {
	return &Handler{
		store:   d.Store,
		st:      d.Storage,
		set:     d.Settings,
		files:   d.Files,
		folders: d.Folders,
		shares:  d.Shares,
		uploads: d.Uploads,
		maint:   d.Maintain,
		tokens:  d.Tokens,
		limiter: auth.NewLimiter(5, 5*time.Minute),
		users:   auth.NewUserCache[*model.User](15 * time.Second),
		started: time.Now(),
	}
}

// --- 统一响应 ---

// apiError 是错误响应体。
type apiError struct {
	Error string `json:"error"`
}

func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, apiError{Error: msg})
}

func ok(c *gin.Context, v any) { c.JSON(http.StatusOK, v) }

// failErr 把领域错误映射为合适的 HTTP 状态码。
func failErr(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "记录不存在")
	case errors.Is(err, store.ErrConflict):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalidInput), errors.Is(err, settings.ErrInvalid):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrInUse):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrState):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, files.ErrCannotPinSelf):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, files.ErrForbidden), errors.Is(err, upload.ErrForbidden):
		fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, upload.ErrTooLarge):
		fail(c, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, upload.ErrBadExt):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, upload.ErrUploadOff):
		fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, upload.ErrGone):
		fail(c, http.StatusGone, err.Error())
	case errors.Is(err, upload.ErrBadIndex):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, upload.ErrOverrun):
		fail(c, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, upload.ErrIncomplete), errors.Is(err, upload.ErrSizeMatch),
		errors.Is(err, upload.ErrChunkBroken):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, context.Canceled):
		fail(c, http.StatusRequestTimeout, "请求已中断")
	default:
		slog.Error("接口处理失败", "path", c.FullPath(), "error", err)
		fail(c, http.StatusInternalServerError, fallback)
	}
}

// bindJSON 解析请求体并限制大小。
func bindJSON(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(dst); err != nil {
		fail(c, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return false
	}
	return true
}

// --- 鉴权 ---

// requireAuth 校验 JWT 并回查用户，把用户挂到 gin.Context。
func (h *Handler) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			fail(c, http.StatusUnauthorized, "未登录")
			return
		}
		claims, err := h.tokens.Parse(strings.TrimPrefix(authz, "Bearer "))
		if err != nil {
			fail(c, http.StatusUnauthorized, "登录状态无效或已过期")
			return
		}
		userID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil {
			fail(c, http.StatusUnauthorized, "登录状态无效或已过期")
			return
		}
		u, err := h.loadUser(c.Request.Context(), userID)
		if err != nil {
			fail(c, http.StatusUnauthorized, "账号不存在或已被删除")
			return
		}
		if !u.Enabled {
			fail(c, http.StatusUnauthorized, auth.ErrUserDisabled.Error())
			return
		}
		// 密码被重置后旧令牌立即失效。
		if !auth.VerifyPwdVer(claims.PwdVer, u.Password) {
			fail(c, http.StatusUnauthorized, "密码已变更，请重新登录")
			return
		}
		c.Set(ctxUserKey, u)
		c.Next()
	}
}

// requireAdmin 要求管理员身份（必须放在 requireAuth 之后）。
func (h *Handler) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		if u == nil || !u.IsAdmin() {
			fail(c, http.StatusForbidden, "需要管理员权限")
			return
		}
		c.Next()
	}
}

// RequireAuth 是导出的鉴权中间件（供 router 装配）。
func (h *Handler) RequireAuth() gin.HandlerFunc { return h.requireAuth() }

// RequireAdmin 是导出的管理员校验中间件（必须放在 RequireAuth 之后）。
func (h *Handler) RequireAdmin() gin.HandlerFunc { return h.requireAdmin() }

// loadUser 带短 TTL 缓存地读取用户。
func (h *Handler) loadUser(ctx context.Context, id int64) (*model.User, error) {
	if u, found := h.users.Get(id); found {
		return u, nil
	}
	u, err := h.store.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	h.users.Put(id, u)
	return u, nil
}

// currentUser 返回当前登录用户（未登录返回 nil）。
func currentUser(c *gin.Context) *model.User {
	v, exists := c.Get(ctxUserKey)
	if !exists {
		return nil
	}
	u, _ := v.(*model.User)
	return u
}

// clientIP 返回客户端 IP（仅在信任代理时采信 X-Forwarded-For）。
func clientIP(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	return host
}

// --- 查询参数辅助 ---

func queryInt(c *gin.Context, key string, def int) int {
	v := strings.TrimSpace(c.Query(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryInt64(c *gin.Context, key string, def int64) int64 {
	v := strings.TrimSpace(c.Query(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func pathInt64(c *gin.Context, key string) (int64, bool) {
	v := strings.TrimSpace(c.Param(key))
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		fail(c, http.StatusBadRequest, "路径参数 "+key+" 必须是正整数")
		return 0, false
	}
	return n, true
}

// normalizePage 规范化分页参数（上限 200，避免一次拉爆数据库）。
func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// paged 组装统一分页响应。
func paged[T any](items []T, total int64, page, size int) gin.H {
	if items == nil {
		items = []T{}
	}
	return gin.H{"items": items, "total": total, "page": page, "page_size": size}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
