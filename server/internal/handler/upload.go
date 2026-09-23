package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/model"
	"lan-drive/internal/upload"
)

// InitUpload 处理 POST /api/uploads/init。
//
// 请求：{"file_name":"报表.xlsx","file_size":12345,"sha256":"..."}
// 响应：UploadSession（含 chunk_size、total_chunks、uploaded[]）
func (h *Handler) InitUpload(c *gin.Context) {
	var req struct {
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
		SHA256   string `json:"sha256"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.FileName) == "" {
		fail(c, http.StatusBadRequest, "文件名不能为空")
		return
	}
	if req.FileSize < 0 {
		fail(c, http.StatusBadRequest, "文件大小不能为负数")
		return
	}
	u := currentUser(c)
	sess, err := h.uploads.Init(c.Request.Context(), upload.InitInput{
		Owner:     u,
		FileName:  req.FileName,
		SizeBytes: req.FileSize,
		SHA256:    req.SHA256,
	})
	if err != nil {
		// init 阶段不合格的请求不会产生任何磁盘写入。
		failErr(c, err, "初始化上传失败")
		return
	}
	ok(c, sess)
}

// PutChunk 处理 PUT /api/uploads/:id/chunks/:idx（请求体为裸二进制）。
func (h *Handler) PutChunk(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		fail(c, http.StatusBadRequest, "缺少上传会话 ID")
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(c.Param("idx")))
	if err != nil || idx < 0 {
		fail(c, http.StatusBadRequest, "分片序号必须是非负整数")
		return
	}

	u := currentUser(c)
	sess, err := h.uploads.Status(c.Request.Context(), id, u)
	if err != nil {
		failErr(c, err, "上传会话不可用")
		return
	}
	// 限制请求体大小，避免超大请求打满磁盘（分片大小 + 1 字节用于探测越界）。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(sess.ChunkSize)+1)
	written, updated, err := h.uploads.WriteChunk(c.Request.Context(), sess, u, idx, c.Request.Body)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			fail(c, http.StatusRequestEntityTooLarge,
				"单个分片不得超过 "+strconv.Itoa(sess.ChunkSize)+" 字节")
			return
		}
		_ = written
		failErr(c, err, "写入分片失败")
		return
	}
	ok(c, gin.H{
		"upload_id":      updated.ID,
		"idx":            idx,
		"size_bytes":     written,
		"uploaded":       updated.Uploaded,
		"received_bytes": updated.ReceivedBytes,
		"total_chunks":   updated.TotalChunks,
	})
}

// GetUpload 处理 GET /api/uploads/:id（断点续传依据）。
func (h *Handler) GetUpload(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		fail(c, http.StatusBadRequest, "缺少上传会话 ID")
		return
	}
	sess, err := h.uploads.Status(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "上传会话不可用")
		return
	}
	ok(c, sess)
}

// CompleteUpload 处理 POST /api/uploads/:id/complete。
func (h *Handler) CompleteUpload(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		fail(c, http.StatusBadRequest, "缺少上传会话 ID")
		return
	}
	res, err := h.uploads.Complete(c.Request.Context(), id, currentUser(c))
	if err != nil {
		failErr(c, err, "合并上传文件失败")
		return
	}
	if res.Created {
		h.audit(c, model.ActUpload, "file", itoa(res.File.ID),
			"上传文件 "+res.File.OriginalNam+"（"+fmtBytes(res.File.SizeBytes)+"）")
	}
	ok(c, res)
}

// CancelUpload 处理 DELETE /api/uploads/:id。
func (h *Handler) CancelUpload(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		fail(c, http.StatusBadRequest, "缺少上传会话 ID")
		return
	}
	if err := h.uploads.Cancel(c.Request.Context(), id, currentUser(c)); err != nil {
		failErr(c, err, "取消上传失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

// UploadConfig 处理 GET /api/uploads/config，返回当前生效的上传策略
// （前端在选择文件前即可用它对体积与类型做即时校验）。
func (h *Handler) UploadConfig(c *gin.Context) {
	cfg := h.set.Get()
	ok(c, gin.H{
		"max_file_size_mb":   cfg.MaxFileSizeMB,
		"max_file_size":      cfg.MaxFileSizeBytes(),
		"chunk_size_mb":      cfg.ChunkSizeMB,
		"chunk_size":         cfg.ChunkSizeBytes(),
		"allowed_extensions": cfg.ExtList(),
		"allow_all":          cfg.AllowsAll(),
		"upload_enabled":     cfg.UploadEnabled,
	})
}
