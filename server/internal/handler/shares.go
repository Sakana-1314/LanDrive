package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/folders"
	"lan-drive/internal/model"
	"lan-drive/internal/shares"
	"lan-drive/internal/storage"
)

// ListFolders 处理 GET /api/folders?owner_id=&folder_id=。
//
// 返回某一层的子目录 + 面包屑。文件由 GET /api/files?folder_id= 另取，
// 两者分开是因为文件列表要支持分页与排序，而目录很轻不需要分页。
func (h *Handler) ListFolders(c *gin.Context) {
	in := folders.ListInput{
		Actor:    currentUser(c),
		OwnerID:  queryInt64(c, "owner_id", 0),
		FolderID: queryInt64(c, "folder_id", 0),
	}
	res, err := h.folders.List(c.Request.Context(), in)
	if err != nil {
		failErr(c, err, "查询文件夹失败")
		return
	}
	ok(c, res)
}

// CreateFolder 处理 POST /api/folders。
//
// 请求：{"name":"报表","parent_id":0,"owner_id":0}
func (h *Handler) CreateFolder(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		ParentID int64  `json:"parent_id"`
		OwnerID  int64  `json:"owner_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	f, err := h.folders.Create(c.Request.Context(), folders.CreateInput{
		Actor:    currentUser(c),
		OwnerID:  req.OwnerID,
		ParentID: req.ParentID,
		Name:     req.Name,
	})
	if err != nil {
		failErr(c, err, "新建文件夹失败")
		return
	}
	ok(c, f)
}

// RenameFolder 处理 PATCH /api/folders/:id。
//
// 请求：{"name":"新名字","parent_id":0}
// parent_id 用于移动到别的目录；只想改名时传当前父目录或省略。
func (h *Handler) RenameFolder(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	in := folders.RenameInput{Actor: currentUser(c), ID: id, NewName: req.Name}
	if req.ParentID != nil {
		in.NewParentID = *req.ParentID
	}
	f, err := h.folders.Rename(c.Request.Context(), in)
	if err != nil {
		failErr(c, err, "重命名文件夹失败")
		return
	}
	ok(c, f)
}

// DeleteFolder 处理 DELETE /api/folders/:id。
// 目录下的文件会被软删（进回收站，管理员可恢复），目录本身从磁盘移除。
func (h *Handler) DeleteFolder(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	n, err := h.folders.Delete(c.Request.Context(), currentUser(c), id)
	if err != nil {
		failErr(c, err, "删除文件夹失败")
		return
	}
	ok(c, gin.H{"ok": true, "soft_deleted_files": n})
}

// --- 分享 ---

// CreateShare 处理 POST /api/shares。
//
// 请求：{"target_type":"file|folder","target_id":1,"expire_days":null}
// expire_days 省略或 null = 永久（默认）；其余只允许 1/3/7/30。
func (h *Handler) CreateShare(c *gin.Context) {
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		ExpireDays *int   `json:"expire_days"`
	}
	if !bindJSON(c, &req) {
		return
	}
	sh, err := h.shares.Create(c.Request.Context(), sharesCreateInput(currentUser(c), req.TargetType, req.TargetID, req.ExpireDays))
	if err != nil {
		failErr(c, err, "创建分享失败")
		return
	}
	ok(c, sh)
}

// ListShares 处理 GET /api/shares?mine=1&page=&page_size=。
//
// 需求：所有人都能看到所有人创建的分享。默认返回全部（带创建者信息）；
// mine=1 时只看自己创建的。
func (h *Handler) ListShares(c *gin.Context) {
	page, size := normalizePage(queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	onlyMine := strings.TrimSpace(c.Query("mine")) == "1"
	items, total, err := h.shares.List(c.Request.Context(), currentUser(c), onlyMine, page, size)
	if err != nil {
		failErr(c, err, "查询分享失败")
		return
	}
	ok(c, paged(items, total, page, size))
}

// RevokeShare 处理 DELETE /api/shares/:id。
func (h *Handler) RevokeShare(c *gin.Context) {
	id, valid := pathInt64(c, "id")
	if !valid {
		return
	}
	if err := h.shares.Revoke(c.Request.Context(), id, currentUser(c)); err != nil {
		failErr(c, err, "撤销分享失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

// ShareExpireOptions 处理 GET /api/shares/options，返回可选有效期。
// 由服务端给出，避免前端硬编码一套值与后端校验不一致。
func (h *Handler) ShareExpireOptions(c *gin.Context) {
	ok(c, gin.H{"expire_days": model.ShareExpireOptions})
}

// --- 免登录访问（不经过 RequireAuth）---

// ResolveShare 处理 GET /api/s/:token。
//
// 这是**唯一**的免登录业务入口。它只认 token，不接受任何 id/路径参数，
// 避免有人拿 token 之外的参数去枚举别人的文件。
func (h *Handler) ResolveShare(c *gin.Context) {
	res, err := h.shares.Resolve(c.Request.Context(), c.Param("token"))
	if err != nil {
		failErr(c, err, "解析分享失败")
		return
	}
	// 状态直接用 200 + status 字段返回：前端要区分
	// 「已过期」「文件已被删除」「链接无效」三种情况，各自文案不同，
	// 用 404 一律表示会让前端无法区分。
	ok(c, gin.H{
		"status":      res.Status,
		"name":        res.Name,
		"size_bytes":  res.SizeBytes,
		"mime":        res.Mime,
		"ext":         res.Ext,
		"kind":        res.Kind,
		"file_count":  res.FileCount,
		"total_bytes": res.TotalBytes,
		"owner_name":  res.OwnerName,
		"target_type": shareTargetType(res),
		"expire_days": shareExpireDays(res),
		"expires_at":  shareExpiresAt(res),
		"created_at":  shareCreatedAt(res),
		"view_count":  shareViewCount(res),
	})
}

// ShareDownload 处理 GET /api/s/:token/download（免登录，附件下载）。
func (h *Handler) ShareDownload(c *gin.Context) {
	token := c.Param("token")
	res, err := h.shares.Resolve(c.Request.Context(), token)
	if err != nil {
		failErr(c, err, "解析分享失败")
		return
	}
	if res.Status != shares.ResolveOK {
		failShareStatus(c, res)
		return
	}

	if res.Share.TargetType == model.ShareTargetFolder {
		h.serveShareFolderZip(c, token, res)
		return
	}

	f, abs, err := h.shares.FileForDownload(c.Request.Context(), token)
	if err != nil {
		failErr(c, err, "文件不存在或已被清理")
		return
	}
	c.Header("Content-Disposition", contentDisposition("attachment", f.OriginalNam, f.Ext))
	c.Header("Content-Type", "application/octet-stream")
	c.File(abs)
}

// SharePreview 处理 GET /api/s/:token/content（免登录，内联内容）。
//
// 安全约束与登录态预览完全一致：只有 storage.IsInlinePreviewable 允许的类型
// 才内联，其余强制附件下发 —— 否则免登录链接可能变成"托管并执行任意 HTML/SVG"
// 的入口。体积上限同样要在这里兜住：只在 /preview 的返回值里写 kind=too-large
// 是不够的，用户直接扒出 /content 地址仍然会让浏览器去渲染超大文件。
func (h *Handler) SharePreview(c *gin.Context) {
	token := c.Param("token")
	res, err := h.shares.Resolve(c.Request.Context(), token)
	if err != nil {
		failErr(c, err, "解析分享失败")
		return
	}
	if res.Status != shares.ResolveOK {
		failShareStatus(c, res)
		return
	}
	f, abs, err := h.shares.FileForDownload(c.Request.Context(), token)
	if err != nil {
		failErr(c, err, "文件不存在或已被清理")
		return
	}
	// 超限即降级为附件：下载仍然可用，只是不再由浏览器内联渲染。
	tooLarge := !h.set.Get().PreviewSizeAllowed(f.SizeBytes)
	inline := sharePreviewable(f.Ext) && !tooLarge
	if inline {
		c.Header("Content-Disposition", contentDisposition("inline", f.OriginalNam, f.Ext))
	} else {
		c.Header("Content-Disposition", contentDisposition("attachment", f.OriginalNam, f.Ext))
	}
	if f.Mime != "" && inline {
		c.Header("Content-Type", f.Mime)
	} else {
		c.Header("Content-Type", "application/octet-stream")
	}
	c.File(abs)
}

// 目录分享打包下载的限制。超限时明确拒绝而不是把服务器打爆。
const (
	maxZipFiles = 2000
	maxZipBytes = int64(2) << 30 // 2 GiB
)

// serveShareFolderZip 把目录分享打包成 zip 流式下发。
//
// 三个要点：
//   - **流式**：边压缩边写响应，不把整个包放内存（目录可能很大）；
//   - 用 zip.Writer 而不是先落盘再发送，避免占用磁盘与清理问题；
//   - 文件名用原始名（zip 头里写 UTF-8 名，Windows 资源管理器能正确显示中文）。
func (h *Handler) serveShareFolderZip(c *gin.Context, token string, res *shares.Resolved) {
	if res.FileCount > maxZipFiles {
		fail(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("该文件夹文件过多（%d 个，上限 %d），请分批下载", res.FileCount, maxZipFiles))
		return
	}
	if res.TotalBytes > maxZipBytes {
		fail(c, http.StatusRequestEntityTooLarge, "该文件夹体积过大（超过 2 GB），请分批下载")
		return
	}

	folder, entries, err := h.shares.FolderFilesForDownload(c.Request.Context(), token)
	if err != nil {
		failErr(c, err, "打包下载失败")
		return
	}
	// 空文件夹也返回一个合法的空 zip，而不是 410：
	// Resolve 已经把"文件夹存在但为空"判为可用（status=ok），
	// 这里若回"已被删除"，同一份分享就会自相矛盾，用户以为是文件丢了。

	zipName := storage.SanitizeName(folder.Name) + ".zip"
	c.Header("Content-Disposition", contentDisposition("attachment", zipName, ".zip"))
	c.Header("Content-Type", "application/zip")

	zw := zip.NewWriter(c.Writer)
	defer func() { _ = zw.Close() }()
	for _, e := range entries {
		if err := writeZipEntry(zw, e); err != nil {
			// 已经开始写响应，无法再改状态码；中断连接让客户端感知失败。
			_ = c.Error(err)
			return
		}
	}
}

// writeZipEntry 把一个文件写进 zip。
func writeZipEntry(zw *zip.Writer, e shares.ArchiveEntry) error {
	f, err := os.Open(e.AbsPath)
	if err != nil {
		// 单个文件缺失不应让整包失败：跳过它继续。
		return nil
	}
	defer func() { _ = f.Close() }()

	// 用 CreateHeader 以便保留修改时间，并声明 UTF-8 文件名。
	hdr := &zip.FileHeader{
		Name:     e.Name,
		Method:   zip.Deflate,
		Modified: e.ModTime,
	}
	hdr.NonUTF8 = false
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// failShareStatus 把非 ok 的分享状态映射成明确的中文错误。
// 关键是让用户能区分「已过期」与「文件已被删除」。
func failShareStatus(c *gin.Context, res *shares.Resolved) {
	switch res.Status {
	case shares.ResolveExpired:
		fail(c, http.StatusGone, "该分享链接已过期")
	case shares.ResolveDeleted:
		fail(c, http.StatusGone, "分享的文件已被删除")
	default:
		fail(c, http.StatusNotFound, "分享链接无效或已被撤销")
	}
}

// sharePreviewable 复用 storage 的判定，保证与登录态预览同一套规则。
func sharePreviewable(ext string) bool { return storage.IsInlinePreviewable(ext) }

// sharesCreateInput 把请求参数转成服务层入参（集中一处，避免两处各拼一遍）。
func sharesCreateInput(actor *model.User, targetType string, targetID int64, expireDays *int) shares.CreateInput {
	return shares.CreateInput{
		Actor:      actor,
		TargetType: targetType,
		TargetID:   targetID,
		ExpireDays: expireDays,
	}
}

// 下面几个小的取值辅助函数：Resolve 的结果里 Share 可能为空
// （token 无效时），调用方只关心展示字段，取值时统一做空值兜底。

func shareTargetType(res *shares.Resolved) string {
	if res.Share == nil {
		return ""
	}
	return res.Share.TargetType
}

func shareExpireDays(res *shares.Resolved) *int {
	if res.Share == nil {
		return nil
	}
	return res.Share.ExpireDays
}

func shareExpiresAt(res *shares.Resolved) *time.Time {
	if res.Share == nil {
		return nil
	}
	return res.Share.ExpiresAt
}

func shareCreatedAt(res *shares.Resolved) *time.Time {
	if res.Share == nil {
		return nil
	}
	t := res.Share.CreatedAt
	return &t
}

func shareViewCount(res *shares.Resolved) int64 {
	if res.Share == nil {
		return 0
	}
	return res.Share.ViewCount
}
