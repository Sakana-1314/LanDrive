package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/auth"
	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/store"
)

// Login 处理 POST /api/auth/login。
//
// 请求：{"employee_no":"1001","password":"..."}
// 响应：{"token":"...","expires_at":"...","user":{...}}
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		EmployeeNo string `json:"employee_no"`
		Password   string `json:"password"`
	}
	if !bindJSON(c, &req) {
		return
	}
	req.EmployeeNo = strings.TrimSpace(req.EmployeeNo)
	if req.EmployeeNo == "" || req.Password == "" {
		fail(c, http.StatusBadRequest, "请填写工号与密码")
		return
	}

	ip := clientIP(c)
	limitKey := ip + "|" + req.EmployeeNo
	if !h.limiter.Allow(limitKey) {
		fail(c, http.StatusTooManyRequests, auth.ErrTooMany.Error())
		return
	}

	u, err := h.store.GetUserByEmployeeNo(c.Request.Context(), req.EmployeeNo)
	if err != nil || u == nil || !auth.CheckPassword(u.Password, req.Password) {
		h.limiter.Fail(limitKey)
		// 统一提示，不暴露工号是否存在。
		fail(c, http.StatusUnauthorized, auth.ErrBadCredentials.Error())
		return
	}
	if !u.Enabled {
		h.limiter.Fail(limitKey)
		fail(c, http.StatusForbidden, auth.ErrUserDisabled.Error())
		return
	}

	h.limiter.Reset(limitKey)
	h.users.Put(u.ID, u)
	_ = h.store.TouchLastLogin(c.Request.Context(), u.ID)

	token, exp, err := h.tokens.Issue(u.ID, u.EmployeeNo, u.Role, u.Password)
	if err != nil {
		failErr(c, err, "签发登录令牌失败")
		return
	}

	// 注意：u 已放入用户缓存，这里必须外发副本，绝不能原地清空密码。
	ok(c, gin.H{"token": token, "expires_at": exp, "user": h.publicWithUsage(c, u)})
}

// Me 处理 GET /api/auth/me，返回当前用户与生效配置。
func (h *Handler) Me(c *gin.Context) {
	u := currentUser(c)
	ok(c, gin.H{"user": h.publicWithUsage(c, u), "settings": h.set.Get()})
}

// publicWithUsage 返回可外发的用户副本，并补齐"我的文件数 / 占用"。
//
// 为什么需要：users 表里没有这两个聚合列，GetUserByID 取到的用户 FileCount 与
// UsedBytes 恒为结构体零值，直接外发会让顶栏永远显示"0 个文件 · 0 B"。
// 必须在**副本**上赋值 —— u 同时被 15 秒 TTL 的用户缓存持有，
// 原地修改会污染缓存，让其它请求读到串味的数据。
func (h *Handler) publicWithUsage(c *gin.Context, u *model.User) *model.User {
	out := u.Public()
	if out == nil {
		return nil
	}
	// 用 ActiveUsageByOwner 而非 CountFilesByOwner：后者不过滤 status，
	// 会把回收站里的文件也算进"我的文件数"，与「我的文件」列表的口径不符。
	// 文件数与占用一次查出，避免两次查询之间状态变化导致数字自相矛盾。
	// 统计失败不该让 /me 整体失败：退化为 0 也比登录态报错强。
	if n, bytes, err := h.store.ActiveUsageByOwner(c.Request.Context(), u.ID); err == nil {
		out.FileCount = n
		out.UsedBytes = bytes
	}
	return out
}

// ChangePassword 处理 POST /api/auth/password。
func (h *Handler) ChangePassword(c *gin.Context) {
	u := currentUser(c)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if !auth.CheckPassword(u.Password, req.OldPassword) {
		fail(c, http.StatusBadRequest, "原密码不正确")
		return
	}
	if req.NewPassword == req.OldPassword {
		fail(c, http.StatusBadRequest, "新密码不能与原密码相同")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	newPwd := hash
	if err := h.store.UpdateUser(c.Request.Context(), u.ID, store.UserUpdate{Password: &newPwd}); err != nil {
		failErr(c, err, "修改密码失败")
		return
	}
	h.users.Invalidate(u.ID)
	ok(c, gin.H{"ok": true})
}

// --- 管理员：用户管理 ---

// ListUsers 处理 GET /api/admin/users。
func (h *Handler) ListUsers(c *gin.Context) {
	page, size := normalizePage(queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	q := strings.TrimSpace(c.Query("q"))
	items, total, err := h.store.ListUsers(c.Request.Context(), q, page, size)
	if err != nil {
		failErr(c, err, "查询用户失败")
		return
	}
	for i := range items {
		items[i] = *items[i].Public()
	}
	ok(c, paged(items, total, page, size))
}

// CreateUser 处理 POST /api/admin/users。
func (h *Handler) CreateUser(c *gin.Context) {
	var req struct {
		EmployeeNo string `json:"employee_no"`
		Name       string `json:"name"`
		Role       string `json:"role"`
		Password   string `json:"password"`
	}
	if !bindJSON(c, &req) {
		return
	}
	req.EmployeeNo = strings.TrimSpace(req.EmployeeNo)
	req.Name = strings.TrimSpace(req.Name)
	if req.EmployeeNo == "" || req.Name == "" {
		fail(c, http.StatusBadRequest, "工号与姓名均不能为空")
		return
	}
	if len(req.EmployeeNo) > 32 {
		fail(c, http.StatusBadRequest, "工号长度不能超过 32 个字符")
		return
	}
	if strings.ContainsAny(req.EmployeeNo, " \t/\\") {
		fail(c, http.StatusBadRequest, "工号不能包含空格或路径分隔符")
		return
	}
	if req.Role != model.RoleAdmin && req.Role != model.RoleUser {
		fail(c, http.StatusBadRequest, "角色只能是 admin 或 user")
		return
	}
	if len([]rune(req.Name)) > 64 {
		fail(c, http.StatusBadRequest, "姓名长度不能超过 64 个字符")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusBadRequest, "密码不合法："+err.Error())
		return
	}

	u := &model.User{
		EmployeeNo: req.EmployeeNo,
		Name:       req.Name,
		Password:   hash,
		Role:       req.Role,
		Enabled:    true,
	}
	if err := h.store.CreateUser(c.Request.Context(), u); err != nil {
		failErr(c, err, "创建用户失败")
		return
	}
	// 目录名依赖自增主键，因此创建后立刻建目录并把相对路径写回数据库。
	dirRel, err := h.st.EnsureUserDir(u.ID)
	if err != nil {
		// 目录建不出来就回滚账号，避免出现没有目录的账号。
		_ = h.store.DeleteUser(c.Request.Context(), u.ID)
		failErr(c, err, "创建用户目录失败")
		return
	}
	u.DirRel = dirRel
	if err := h.store.SetUserDirRel(c.Request.Context(), u.ID, dirRel); err != nil {
		_ = h.store.DeleteUser(c.Request.Context(), u.ID)
		failErr(c, err, "初始化用户目录失败")
		return
	}

	ok(c, u.Public())
}

// UpdateUser 处理 PATCH /api/admin/users/:id。
func (h *Handler) UpdateUser(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Role    *string `json:"role"`
		Enabled *bool   `json:"enabled"`
	}
	if !bindJSON(c, &req) {
		return
	}
	target, err := h.store.GetUserByID(c.Request.Context(), id)
	if err != nil {
		failErr(c, err, "用户不存在")
		return
	}
	actor := currentUser(c)

	up := store.UserUpdate{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 64 {
			fail(c, http.StatusBadRequest, "姓名不能为空且不超过 64 个字符")
			return
		}
		up.Name = &name
	}
	if req.Role != nil {
		if *req.Role != model.RoleAdmin && *req.Role != model.RoleUser {
			fail(c, http.StatusBadRequest, "角色只能是 admin 或 user")
			return
		}
		if target.ID == actor.ID && *req.Role != model.RoleAdmin {
			fail(c, http.StatusBadRequest, "不能取消自己的管理员角色")
			return
		}
		if target.IsAdmin() && *req.Role != model.RoleAdmin {
			if err := h.ensureNotLastAdmin(c, target); err != nil {
				return
			}
		}
		up.Role = req.Role
	}
	if req.Enabled != nil {
		if target.ID == actor.ID && !*req.Enabled {
			fail(c, http.StatusBadRequest, "不能停用自己的账号")
			return
		}
		if target.IsAdmin() && !*req.Enabled {
			if err := h.ensureNotLastAdmin(c, target); err != nil {
				return
			}
		}
		up.Enabled = req.Enabled
	}

	if err := h.store.UpdateUser(c.Request.Context(), id, up); err != nil {
		failErr(c, err, "更新用户失败")
		return
	}
	h.users.Invalidate(id)

	updated, err := h.store.GetUserByID(c.Request.Context(), id)
	if err != nil {
		failErr(c, err, "读取更新后的用户失败")
		return
	}
	ok(c, updated.Public())
}

// ResetPassword 处理 POST /api/admin/users/:id/password。
func (h *Handler) ResetPassword(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	var req struct {
		NewPassword string `json:"new_password"`
	}
	if !bindJSON(c, &req) {
		return
	}
	// 先确认账号存在：UpdateUser 对不存在的 id 也能返回 ErrNotFound，
	// 但这里给出更贴切的提示，且避免为不存在的账号做无用的哈希计算。
	if _, err := h.store.GetUserByID(c.Request.Context(), id); err != nil {
		failErr(c, err, "用户不存在")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		fail(c, http.StatusBadRequest, "密码不合法："+err.Error())
		return
	}
	if err := h.store.UpdateUser(c.Request.Context(), id, store.UserUpdate{Password: &hash}); err != nil {
		failErr(c, err, "重置密码失败")
		return
	}
	// 使该用户已签发的令牌立即失效。
	h.users.Invalidate(id)
	ok(c, gin.H{"ok": true})
}

// DeleteUser 处理 DELETE /api/admin/users/:id。
//
// 默认拒绝删除仍有文件的账号（409 并附文件数）；显式传 ?purge_files=1 时
// 先彻底删除其全部文件（磁盘 + 记录）再删账号。
func (h *Handler) DeleteUser(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	actor := currentUser(c)
	if id == actor.ID {
		fail(c, http.StatusBadRequest, "不能删除自己的账号")
		return
	}
	target, err := h.store.GetUserByID(c.Request.Context(), id)
	if err != nil {
		failErr(c, err, "用户不存在")
		return
	}
	if target.IsAdmin() {
		if err := h.ensureNotLastAdmin(c, target); err != nil {
			return
		}
	}

	fileCount, err := h.store.CountFilesByOwner(c.Request.Context(), id)
	if err != nil {
		failErr(c, err, "统计用户文件失败")
		return
	}
	purge := strings.TrimSpace(c.Query("purge_files")) == "1"
	if fileCount > 0 && !purge {
		c.JSON(http.StatusConflict, gin.H{
			"error":      fmt.Sprintf("该账号仍有 %d 个文件，删除后这些文件将无法恢复。如需一并删除请勾选“同时彻底删除文件”", fileCount),
			"file_count": fileCount,
		})
		return
	}

	// 先终止该用户未完成的上传会话与分片。
	sessions, err := h.store.ListUploadSessionsByOwner(c.Request.Context(), id, model.UploadUploading)
	if err != nil {
		failErr(c, err, "清理该账号上传会话失败")
		return
	}
	for i := range sessions {
		_ = h.uploads.Abort(c.Request.Context(), &sessions[i])
	}

	deleted := 0
	if fileCount > 0 {
		n, err := h.files.PurgeByOwner(c.Request.Context(), id)
		if err != nil {
			failErr(c, err, fmt.Sprintf("删除该账号文件时失败（已删除 %d 个）", n))
			return
		}
		deleted = n
	}
	if err := h.store.DeleteUser(c.Request.Context(), id); err != nil {
		failErr(c, err, "删除账号失败")
		return
	}
	// 账号删除后再删目录，避免遗留空目录。
	if err := h.maint.CleanupUserDir(id); err != nil {
		// 目录删除失败不影响账号删除结果，记录日志由孤儿扫描兜底。
	}
	h.users.Invalidate(id)
	ok(c, gin.H{"ok": true, "deleted_files": deleted})
}

// GetSettings 处理 GET /api/admin/settings。
func (h *Handler) GetSettings(c *gin.Context) {
	ok(c, h.set.Get())
}

// settingsPatch 是 PUT /api/admin/settings 的请求体。
type settingsPatch struct {
	MaxFileSizeMB     *int    `json:"max_file_size_mb"`
	AllowedExtensions *string `json:"allowed_extensions"`
	RetentionDays     *int    `json:"retention_days"`
	TrashDays         *int    `json:"trash_days"`
	ChunkSizeMB       *int    `json:"chunk_size_mb"`
	UploadEnabled     *bool   `json:"upload_enabled"`
}

// UpdateSettings 处理 PUT /api/admin/settings（部分更新）。
func (h *Handler) UpdateSettings(c *gin.Context) {
	var req settingsPatch
	if !bindJSON(c, &req) {
		return
	}
	patch := settings.Patch{
		MaxFileSizeMB:     req.MaxFileSizeMB,
		AllowedExtensions: req.AllowedExtensions,
		RetentionDays:     req.RetentionDays,
		TrashDays:         req.TrashDays,
		ChunkSizeMB:       req.ChunkSizeMB,
		UploadEnabled:     req.UploadEnabled,
	}
	if patch.Empty() {
		fail(c, http.StatusBadRequest, "没有需要更新的配置项")
		return
	}
	next, err := h.set.Update(c.Request.Context(), patch)
	if err != nil {
		failErr(c, err, "更新配置失败")
		return
	}
	ok(c, next)
}

func describeExt(s string) string {
	if strings.TrimSpace(s) == "" {
		return "全部"
	}
	return s
}

// ensureNotLastAdmin 阻止把系统里最后一个可用管理员降级或删除。
func (h *Handler) ensureNotLastAdmin(c *gin.Context, _ *model.User) error {
	n, err := h.store.CountAdmins(c.Request.Context())
	if err != nil {
		failErr(c, err, "校验管理员数量失败")
		return err
	}
	if n <= 1 {
		err := fmt.Errorf("系统至少需要保留一个启用状态的管理员")
		fail(c, http.StatusBadRequest, err.Error())
		return err
	}
	return nil
}
