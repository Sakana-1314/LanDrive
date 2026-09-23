package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/files"
	"lan-drive/internal/model"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// ListFiles 处理 GET /api/files。
//
// 查询参数：scope=all|mine、owner_id、q、ext、page、page_size、sort、order。
func (h *Handler) ListFiles(c *gin.Context) {
	page, size := normalizePage(queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	opt := files.ListOptions{
		Actor:    currentUser(c),
		Scope:    strings.TrimSpace(c.Query("scope")),
		OwnerID:  queryInt64(c, "owner_id", 0),
		Status:   model.StatusActive,
		Keyword:  c.Query("q"),
		Ext:      c.Query("ext"),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
		Page:     page,
		PageSize: size,
	}
	res, err := h.files.List(c.Request.Context(), opt)
	if err != nil {
		failErr(c, err, "查询文件列表失败")
		return
	}
	ok(c, res)
}

// ListOwners 处理 GET /api/files/owners，返回用户目录树数据。
func (h *Handler) ListOwners(c *gin.Context) {
	items, err := h.files.Owners(c.Request.Context())
	if err != nil {
		failErr(c, err, "查询用户目录失败")
		return
	}
	ok(c, gin.H{"items": items, "total": len(items)})
}

// GetFile 处理 GET /api/files/:id。
func (h *Handler) GetFile(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.Get(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "查询文件失败")
		return
	}
	ok(c, f)
}

// PreviewInfo 处理 GET /api/files/:id/preview，告诉前端该用什么方式预览。
func (h *Handler) PreviewInfo(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.Get(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "查询文件失败")
		return
	}
	kind := previewKind(f.Ext)
	ok(c, gin.H{
		"id":          f.ID,
		"name":        f.OriginalNam,
		"ext":         f.Ext,
		"size_bytes":  f.SizeBytes,
		"mime":        f.Mime,
		"kind":        kind,
		"content_url": fmt.Sprintf("/api/files/%d/content", f.ID),
		"note":        previewNote(kind, f.Ext),
	})
}

// previewKind 判定前端预览方式。
//
// 与 storage.IsInlinePreviewable 保持一致：只有服务端允许内联的类型才会
// 走浏览器原生渲染（pdf/图片/音视频）；其余类型即使前端能解析，也一律
// 以附件形式下发，避免同源脚本执行风险（例如 SVG、HTML）。
func previewKind(ext string) string {
	e := storage.NormalizeExt(ext)
	switch e {
	// 纯前端库解析，服务端按附件下发，前端取 blob 后本地渲染。
	case ".docx":
		return "docx"
	case ".xlsx":
		return "xlsx"
	case ".pptx":
		return "pptx"
	}
	if !storage.IsInlinePreviewable(e) {
		// 旧版 Office 二进制格式给出更明确的提示。
		if e == ".doc" || e == ".xls" || e == ".ppt" {
			return "legacy-office"
		}
		if storage.IsText(e) {
			return "text"
		}
		return "unsupported"
	}
	switch e {
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".tif", ".tiff":
		return "image"
	case ".mp4", ".webm", ".mov", ".ogg":
		return "video"
	default:
		return "audio"
	}
}

func previewNote(kind, ext string) string {
	switch kind {
	case "legacy-office":
		return "旧版 Office 二进制格式（.doc/.xls/.ppt）无法在浏览器中直接解析，请下载后查看，或用 Office 另存为 .docx/.xlsx/.pptx 再上传"
	case "unsupported":
		return "该格式暂不支持在线预览，请下载后查看"
	}
	return ""
}

// RenameFile 处理 PATCH /api/files/:id。
func (h *Handler) RenameFile(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	var req struct {
		OriginalName string `json:"original_name"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.OriginalName) == "" {
		fail(c, http.StatusBadRequest, "文件名不能为空")
		return
	}
	f, err := h.files.Rename(c.Request.Context(), id, req.OriginalName, currentUser(c))
	if err != nil {
		failErr(c, err, "重命名失败")
		return
	}
	h.audit(c, model.ActRename, "file", itoa(f.ID),
		fmt.Sprintf("重命名为 %s", f.OriginalNam))
	ok(c, f)
}

// DeleteFile 处理 DELETE /api/files/:id（属主软删除，进回收站）。
func (h *Handler) DeleteFile(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.SoftDelete(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "删除失败")
		return
	}
	h.audit(c, model.ActDelete, "file", itoa(f.ID),
		fmt.Sprintf("删除文件 %s（进入回收站，%d 天后彻底删除）", f.OriginalNam, h.set.Get().TrashDays))
	ok(c, gin.H{"ok": true, "file": f})
}

// ServeContent 处理 GET /api/files/:id/content（内联，支持 Range，供预览与播放）。
func (h *Handler) ServeContent(c *gin.Context) {
	h.serveFile(c, false)
}

// Download 处理 GET /api/files/:id/download（附件下载）。
func (h *Handler) Download(c *gin.Context) {
	h.serveFile(c, true)
}

// serveFile 是内容与下载两个接口的共同实现。
//
// 安全要点：
//   - 只有数据库中的相对路径会被使用，绝不拼接用户输入；
//   - 不允许内联预览的类型强制以附件形式下发，避免浏览器执行存储内容。
func (h *Handler) serveFile(c *gin.Context, asAttachment bool) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.Get(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "文件不存在或已被清理")
		return
	}

	abs, err := h.st.Abs(f.RelPath)
	if err != nil {
		failErr(c, err, "文件路径不合法")
		return
	}
	fh, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			fail(c, http.StatusNotFound, "文件已不存在，可能已被清理任务删除")
			return
		}
		failErr(c, err, "打开文件失败")
		return
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		failErr(c, err, "读取文件信息失败")
		return
	}

	ctype := f.Mime
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	inline := !asAttachment && storage.IsInlinePreviewable(f.Ext)
	if !inline {
		ctype = "application/octet-stream"
	}
	c.Header("Content-Type", ctype)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Accept-Ranges", "bytes")
	if inline {
		c.Header("Content-Disposition", contentDisposition("inline", f.OriginalNam, f.Ext))
	} else {
		c.Header("Content-Disposition", contentDisposition("attachment", f.OriginalNam, f.Ext))
	}
	if asAttachment {
		// 下载动作记审计日志（预览不记，避免日志被刷爆）。
		h.audit(c, model.ActDownload, "file", itoa(f.ID), "下载文件 "+f.OriginalNam)
	}
	// ServeContent 负责 Range / If-Modified-Since / ETag 等语义。
	http.ServeContent(c.Writer, c.Request, f.OriginalNam, st.ModTime(), fh)
}

// contentDisposition 生成兼容中文文件名的 Content-Disposition。
//
// 同时给出 ASCII 回退名与 RFC 5987 的 filename*，兼容各浏览器。
func contentDisposition(kind, name, ext string) string {
	ascii := asciiFallbackName(name, ext)
	encoded := url.PathEscape(name)
	// PathEscape 不转义部分字符，按 RFC 5987 再补齐。
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, encoded)
}

// asciiFallbackName 生成纯 ASCII 的回退文件名（保留扩展名）。
//
// 用途：极老的浏览器不支持 RFC 5987 的 filename*，只会用 filename= 里的值。
// 中文名会被全部丢弃，因此必须保证「丢掉名字后仍是合法文件名」——
// 否则会出现 filename=".txt" 这种只有扩展名、没有主名的畸形结果。
func asciiFallbackName(name, ext string) string {
	ext = storage.NormalizeExt(ext)

	// 先剥离扩展名，回退名的主干单独取 ASCII（避免把点也带进主干）。
	base := name
	if ext != "" && strings.HasSuffix(strings.ToLower(base), ext) {
		base = base[:len(base)-len(ext)]
	}

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ', r == '(', r == ')':
			b.WriteRune(r)
		default:
			// 非 ASCII 与危险字符（含路径分隔符、点）一律丢弃。
		}
	}
	// 丢弃非 ASCII 后常留下连续空格（如 "2024 年度 Report" → "2024  Report"），
	// 这里折叠成单个空格，避免文件名出现难看的空洞。
	stem := strings.Join(strings.Fields(b.String()), " ")
	if len(stem) > 100 {
		stem = strings.TrimSpace(stem[:100])
	}
	if stem == "" {
		// 原名没有任何可用 ASCII 字符（纯中文、纯 emoji 等）：
		// 用通用名兜底；真实的文件名由 filename*= 提供。
		stem = "download"
	}
	return stem + ext
}

// Stats 处理 GET /api/admin/stats。
func (h *Handler) Stats(c *gin.Context) {
	st, err := h.store.Stats(c.Request.Context(), time.Now().UTC())
	if err != nil {
		failErr(c, err, "统计失败")
		return
	}
	ok(c, st)
}

// ExtStats 处理 GET /api/admin/stats/ext，返回文件类型分布。
func (h *Handler) ExtStats(c *gin.Context) {
	items, err := h.store.ListExtStats(c.Request.Context(), 20)
	if err != nil {
		failErr(c, err, "统计文件类型失败")
		return
	}
	ok(c, gin.H{"items": items})
}

// ScanStorage 处理 POST /api/admin/storage/scan（只读一致性扫描）。
func (h *Handler) ScanStorage(c *gin.Context) {
	rep, err := h.maint.ScanStorage(c.Request.Context())
	if err != nil {
		failErr(c, err, "存储一致性扫描失败")
		return
	}
	h.audit(c, model.ActStorageScan, "storage", "",
		fmt.Sprintf("一致性扫描：孤儿 %d，缺失 %d，非法路径 %d",
			len(rep.Orphans), len(rep.Missing), len(rep.Invalid)))
	ok(c, rep)
}

// AdminListFiles 处理 GET /api/admin/files（可查 active / trashed / all）。
func (h *Handler) AdminListFiles(c *gin.Context) {
	page, size := normalizePage(queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	status := strings.TrimSpace(c.Query("status"))
	switch status {
	case model.StatusActive, model.StatusTrashed, "all":
	default:
		status = model.StatusActive
	}
	opt := files.ListOptions{
		Actor:    currentUser(c),
		OwnerID:  queryInt64(c, "owner_id", 0),
		Status:   status,
		Keyword:  c.Query("q"),
		Ext:      c.Query("ext"),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
		Page:     page,
		PageSize: size,
	}
	res, err := h.files.List(c.Request.Context(), opt)
	if err != nil {
		failErr(c, err, "查询文件列表失败")
		return
	}
	ok(c, res)
}

// RestoreFile 处理 POST /api/admin/files/:id/restore。
func (h *Handler) RestoreFile(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.Restore(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "恢复失败")
		return
	}
	h.audit(c, model.ActRestore, "file", itoa(f.ID),
		fmt.Sprintf("恢复文件 %s（新到期时间 %s）", f.OriginalNam, f.ExpiresAt.Format(time.RFC3339)))
	ok(c, f)
}

// PurgeFile 处理 DELETE /api/admin/files/:id（立即彻底删除）。
func (h *Handler) PurgeFile(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	f, err := h.files.Purge(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "彻底删除失败")
		return
	}
	h.audit(c, model.ActPurge, "file", itoa(f.ID), "彻底删除文件 "+f.OriginalNam)
	ok(c, gin.H{"ok": true})
}

// ListLogs 处理 GET /api/admin/logs。
func (h *Handler) ListLogs(c *gin.Context) {
	page, size := normalizePage(queryInt(c, "page", 1), queryInt(c, "page_size", 30))
	q := store.LogQuery{
		Action:   strings.TrimSpace(c.Query("action")),
		Keyword:  c.Query("q"),
		UserID:   queryInt64(c, "user_id", 0),
		Page:     page,
		PageSize: size,
	}
	// days 预设：1 / 7 / 30；也可用 from/to 显式指定（RFC3339）。
	if days := queryInt(c, "days", 0); days > 0 {
		from := time.Now().UTC().AddDate(0, 0, -days)
		q.From = &from
	} else {
		if v := strings.TrimSpace(c.Query("from")); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				q.From = &t
			}
		}
		if v := strings.TrimSpace(c.Query("to")); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				q.To = &t
			}
		}
	}
	items, total, err := h.store.ListLogs(c.Request.Context(), q)
	if err != nil {
		failErr(c, err, "查询审计日志失败")
		return
	}
	ok(c, paged(items, total, page, size))
}

// LogActions 处理 GET /api/admin/logs/actions（筛选下拉）。
func (h *Handler) LogActions(c *gin.Context) {
	items := []string{
		model.ActLogin, model.ActLoginFailed, model.ActUpload, model.ActRename,
		model.ActDelete, model.ActRestore, model.ActPurge, model.ActDownload,
		model.ActUserCreate, model.ActUserUpdate, model.ActUserDelete,
		model.ActPasswordChange, model.ActPasswordReset, model.ActSettingsUpdate,
		model.ActMaintainExpire, model.ActMaintainPurge, model.ActMaintainOrphan,
		model.ActStorageScan,
	}
	ok(c, gin.H{"items": items})
}

// Health 处理 GET /api/health（无需登录，供容器健康检查与运维探活）。
func (h *Handler) Health(c *gin.Context) {
	out := gin.H{
		"status":     "ok",
		"service":    "lan-drive-api",
		"uptime_s":   int(time.Since(h.started).Seconds()),
		"data_root":  h.st.Root(),
		"schema_ver": 0,
	}
	if v, err := h.store.SchemaVersion(c.Request.Context()); err == nil {
		out["schema_ver"] = v
	} else {
		out["status"] = "degraded"
		out["error"] = "数据库不可用：" + err.Error()
		c.JSON(http.StatusServiceUnavailable, out)
		return
	}
	ok(c, out)
}
