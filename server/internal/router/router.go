// Package router 装配 Gin 路由。
//
// 本服务是**纯内网 API**：不托管任何前端页面。
// 前端（web/）部署在公网，通过跨域请求访问这里，因此：
//   - 必须正确响应 CORS 预检请求（含 Authorization 头）；
//   - 允许的 Origin 由 LANDRIVE_CORS_ALLOW 白名单决定；
//   - 未在白名单内的跨域请求不返回 CORS 头，浏览器会拦截并提示
//     「无法在此网络下使用，请更换网络再试！」（前端统一处理）。
package router

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/config"
	"lan-drive/internal/handler"
)

// New 构造 gin.Engine（纯 API，无静态资源与 SPA 回退）。
func New(h *handler.Handler, cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), accessLogger())

	// 跨域必须在路由之前处理：OPTIONS 预检请求不会命中任何业务路由。
	r.Use(corsMiddleware(cfg.CORSAllow, cfg.TrustProxy))

	if cfg.TrustProxy {
		// 明确信任反向代理时，ClientIP 才会采信 X-Forwarded-For。
		_ = r.SetTrustedProxies([]string{"0.0.0.0/0", "::/0"})
	} else {
		_ = r.SetTrustedProxies(nil)
	}

	api := r.Group("/api")
	{
		// 健康检查无需登录，供容器健康检查与前端探测连通性使用。
		// 前端用它区分「接口不可达（跨网络）」与「账号密码错误」。
		api.GET("/health", h.Health)
		// 预检请求的统一兜底（正常由 corsMiddleware 提前结束）。
		api.OPTIONS("/*any", func(c *gin.Context) { c.Status(http.StatusNoContent) })

		// 认证（登录必须可跨域访问）
		api.POST("/auth/login", h.Login)

		// 需要登录
		authed := api.Group("", h.RequireAuth())
		{
			authed.GET("/auth/me", h.Me)
			authed.POST("/auth/password", h.ChangePassword)

			// 文件：所有登录用户可读、可预览、可下载任意人的文件
			authed.GET("/files", h.ListFiles)
			authed.GET("/files/owners", h.ListOwners)
			// 置顶是"每人各一份"的个人偏好，因此放在登录档而非管理员档。
			authed.PUT("/files/owners/:id/pin", h.PinOwner)
			authed.DELETE("/files/owners/:id/pin", h.UnpinOwner)
			authed.GET("/files/:id", h.GetFile)
			authed.GET("/files/:id/preview", h.PreviewInfo)
			authed.GET("/files/:id/content", h.ServeContent)
			authed.GET("/files/:id/download", h.Download)
			authed.PATCH("/files/:id", h.RenameFile)
			authed.DELETE("/files/:id", h.DeleteFile)

			// 分片上传
			authed.GET("/uploads/config", h.UploadConfig)
			authed.POST("/uploads/init", h.InitUpload)
			authed.GET("/uploads/:id", h.GetUpload)
			authed.PUT("/uploads/:id/chunks/:idx", h.PutChunk)
			authed.POST("/uploads/:id/complete", h.CompleteUpload)
			authed.DELETE("/uploads/:id", h.CancelUpload)

			// 管理端（仅管理员）
			admin := authed.Group("/admin", h.RequireAdmin())
			{
				admin.GET("/users", h.ListUsers)
				admin.POST("/users", h.CreateUser)
				admin.PATCH("/users/:id", h.UpdateUser)
				admin.POST("/users/:id/password", h.ResetPassword)
				admin.DELETE("/users/:id", h.DeleteUser)

				admin.GET("/settings", h.GetSettings)
				admin.PUT("/settings", h.UpdateSettings)

				admin.GET("/files", h.AdminListFiles)
				admin.POST("/files/:id/restore", h.RestoreFile)
				admin.DELETE("/files/:id", h.PurgeFile)

				admin.GET("/stats", h.Stats)
				admin.GET("/stats/ext", h.ExtStats)
				admin.POST("/storage/scan", h.ScanStorage)
			}
		}
	}

	// 任何非 /api 路径都明确告知：本服务只提供接口，不提供页面。
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{
			"error": "本服务仅提供内网接口（/api/**），不提供网页。请访问前端站点。",
			"docs":  "/api/health",
		})
	})
	return r
}

// accessLogger 输出简洁可读的访问日志。
func accessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		code := c.Writer.Status()
		level := "访问"
		if code >= 400 && code < 500 {
			level = "提示"
		} else if code >= 500 {
			level = "告警"
		}
		fmt.Printf("[%s] %s | %s %s | %d | %s | %s\n",
			level, time.Now().Format("2006-01-02 15:04:05"),
			c.Request.Method, c.Request.URL.Path, code,
			time.Since(start).Round(time.Millisecond), c.ClientIP())
	}
}

// corsMiddleware 按白名单放行跨域请求（前端部署在公网，API 在内网）。
//
// 关键点：
//   - 预检请求（OPTIONS）必须在路由前直接返回 204，否则永远匹配不到路由，
//     浏览器会报 "CORS preflight did not succeed"；
//   - 必须允许 Authorization 请求头，否则携带 Bearer 令牌的请求会被拦截；
//   - 下载/预览是二进制流，需暴露 Content-Disposition 让前端能读到文件名；
//   - 白名单为空 = 不放行任何跨域（同源部署或本地调试走 vite 代理时无需 CORS）。
func corsMiddleware(allow []string, trustProxy bool) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allow))
	allowAll := false
	for _, o := range allow {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
			continue
		}
		if o != "" {
			allowed[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			// 不使用 Cookie 会话（令牌走 Authorization 头），因此不开启凭据模式，
			// 也避免了 "Allow-Origin: * 与 credentials 不能共存" 的限制。
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Requested-With")
			c.Header("Access-Control-Expose-Headers", "Content-Disposition,Content-Length,Content-Range,Accept-Ranges")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			// 预检请求到此结束，不进入业务路由。
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
