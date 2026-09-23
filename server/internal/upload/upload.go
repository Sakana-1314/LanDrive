// Package upload 实现分片上传：初始化、写入分片、查询进度、合并入库、取消。
//
// 关键约束（与 docs/design.md 第 6 节一致）：
//   - 分片大小由服务端配置决定，客户端必须服从；
//   - init 阶段即校验体积与扩展名，不合格的请求不产生任何磁盘写入；
//   - 同一分片重复上传是幂等的；
//   - complete 只会成功一次（CAS 状态迁移），重复调用返回已生成的文件。
package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// 领域错误。handler 层据此映射 HTTP 状态码。
var (
	ErrForbidden   = errors.New("无权操作该上传会话")
	ErrTooLarge    = errors.New("文件超过单文件体积上限")
	ErrBadExt      = errors.New("该文件类型不被允许上传")
	ErrUploadOff   = errors.New("管理员已暂停上传功能")
	ErrBadIndex    = errors.New("分片序号超出范围")
	ErrOverrun     = errors.New("累计上传字节超过声明的文件大小")
	ErrIncomplete  = errors.New("分片未上传完整，无法合并")
	ErrSizeMatch   = errors.New("分片累计大小与声明的大小不一致")
	ErrGone        = errors.New("上传会话不存在或已被清理")
	ErrChunkBroken = errors.New("分片文件缺失，请重新上传该分片")
)

// SessionTTL 是未活动会话的保留时间。
const SessionTTL = 24 * time.Hour

// Service 分片上传服务。
type Service struct {
	store     *store.Store
	st        *storage.Storage
	set       *settings.Service
	decorator Decorator
}

// New 构造服务。decorate 可为 nil（此时直接返回数据库记录）。
func New(s *store.Store, st *storage.Storage, set *settings.Service, decorate Decorator) *Service {
	return &Service{store: s, st: st, set: set, decorator: decorate}
}

// decorateFile 补全展示字段（未注入装饰器时原样返回副本）。
func (s *Service) decorateFile(f *model.File, actor *model.User) *model.File {
	if s.decorator == nil {
		out := *f
		return &out
	}
	out := s.decorator(f, actor)
	return &out
}

// InitInput 是 init 请求的业务入参。
type InitInput struct {
	Owner      *model.User
	FileName   string
	SizeBytes  int64
	SHA256     string
	ChunkSizeM int64
}

// Init 创建（或复用）一个上传会话。
func (s *Service) Init(ctx context.Context, in InitInput) (*model.UploadSession, error) {
	cfg := s.set.Get()
	if !cfg.UploadEnabled {
		return nil, ErrUploadOff
	}
	if in.Owner == nil {
		return nil, ErrForbidden
	}
	if in.SizeBytes < 0 {
		return nil, fmt.Errorf("%w: 文件大小不能为负", store.ErrInvalidInput)
	}
	if in.SizeBytes > cfg.MaxFileSizeBytes() {
		return nil, fmt.Errorf("%w: 上限为 %d MB，当前 %d MB", ErrTooLarge,
			cfg.MaxFileSizeMB, bytesToMB(in.SizeBytes))
	}

	name := storage.SanitizeName(in.FileName)
	ext := storage.ExtOf(name)
	if !cfg.Allows(ext) {
		return nil, fmt.Errorf("%w: %s。允许的类型：%s", ErrBadExt,
			displayExt(ext), cfg.AllowedExtensions)
	}

	sha := normalizeSHA(in.SHA256)
	// 断点续传：同属主 + 同名 + 同大小（+ 同 sha）的未完成会话直接复用。
	if old, err := s.store.FindOpenSession(ctx, in.Owner.ID, name, in.SizeBytes, sha); err == nil {
		if fresh, ferr := s.decorate(ctx, old); ferr == nil {
			return fresh, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	chunkSize := cfg.ChunkSizeBytes()
	if chunkSize <= 0 {
		chunkSize = 4 << 20
	}
	total := TotalChunks(in.SizeBytes, chunkSize)

	sess := &model.UploadSession{
		ID:           strings.ReplaceAll(uuid.NewString(), "-", ""),
		OwnerID:      in.Owner.ID,
		OriginalName: name,
		Ext:          ext,
		SizeBytes:    in.SizeBytes,
		ChunkSize:    int(chunkSize),
		TotalChunks:  total,
		Status:       model.UploadUploading,
		DirRel:       in.Owner.DirRel,
		SHA256:       sha,
	}
	if sess.DirRel == "" {
		sess.DirRel = storage.UserDirRel(in.Owner.ID)
	}
	if err := s.store.CreateUploadSession(ctx, sess); err != nil {
		return nil, err
	}
	return s.decorate(ctx, sess)
}

// WriteChunk 写入一个分片。返回写入的字节数与（更新后的）会话。
//
// limit 为 chunkSize + 1：多出的 1 字节用于判断「实际超出」。
func (s *Service) WriteChunk(ctx context.Context, sess *model.UploadSession, actor *model.User, idx int, body io.Reader) (int64, *model.UploadSession, error) {
	if err := s.checkOwner(sess, actor); err != nil {
		return 0, nil, err
	}
	if sess.Status != model.UploadUploading {
		return 0, nil, fmt.Errorf("%w: 会话已结束", store.ErrState)
	}
	if idx < 0 || idx >= sess.TotalChunks {
		return 0, nil, fmt.Errorf("%w: 序号 %d 不在 0..%d 内", ErrBadIndex, idx, sess.TotalChunks-1)
	}

	rel := storage.ChunkRel(sess.ID, idx)
	maxChunk := int64(sess.ChunkSize) + 1
	written, err := s.st.WriteChunk(rel, body, maxChunk)
	if err != nil {
		return written, nil, err
	}
	if written > int64(sess.ChunkSize) {
		_ = s.st.Remove(rel)
		return written, nil, fmt.Errorf("%w: 单个分片不得超过 %d 字节", ErrOverrun, sess.ChunkSize)
	}

	if err := s.store.UpsertChunk(ctx, sess.ID, idx, written, rel); err != nil {
		return written, nil, err
	}

	// 用累计大小做硬校验：超过声明大小立即拒绝。
	fresh, err := s.store.GetUploadSession(ctx, sess.ID)
	if err != nil {
		return written, nil, err
	}
	if fresh.ReceivedBytes > fresh.SizeBytes {
		return written, nil, fmt.Errorf("%w: 已上传 %d 字节，声明 %d 字节", ErrOverrun,
			fresh.ReceivedBytes, fresh.SizeBytes)
	}
	out, err := s.decorate(ctx, fresh)
	if err != nil {
		return written, nil, err
	}
	return written, out, nil
}

// Status 返回会话当前状态与已上传分片。
func (s *Service) Status(ctx context.Context, id string, actor *model.User) (*model.UploadSession, error) {
	sess, err := s.store.GetUploadSession(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrGone
		}
		return nil, err
	}
	if err := s.checkOwner(sess, actor); err != nil {
		return nil, err
	}
	return s.decorate(ctx, sess)
}

// CompleteResult 是合并成功后的结果。
type CompleteResult struct {
	File    *model.File `json:"file"`
	Created bool        `json:"created"` // false 表示重复调用，返回已存在的文件
	SHA256  string      `json:"sha256"`  // 服务端实际计算的校验值
}

// Decorator 由 files 服务实现：补全 days_left / is_mine / can_edit 等展示字段。
// 用函数注入避免 upload → files 的包循环依赖。
type Decorator func(f *model.File, actor *model.User) model.File

// Complete 校验分片齐全后合并成最终文件并入库。
func (s *Service) Complete(ctx context.Context, id string, actor *model.User) (*CompleteResult, error) {
	sess, err := s.store.GetUploadSession(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrGone
		}
		return nil, err
	}
	if err := s.checkOwner(sess, actor); err != nil {
		return nil, err
	}
	if sess.Status == model.UploadDone {
		// 幂等：重复 complete 直接返回首次生成的文件（会话里记录了 file_id）。
		if f, ferr := s.findCompletedFile(ctx, sess); ferr == nil {
			return &CompleteResult{File: s.decorateFile(f, actor), Created: false, SHA256: f.SHA256}, nil
		}
		return nil, fmt.Errorf("%w: 会话已完成但产物文件已被删除", store.ErrState)
	}
	if sess.Status != model.UploadUploading {
		return nil, fmt.Errorf("%w: 会话状态为 %s", store.ErrState, sess.Status)
	}

	// 再次校验策略：配置可能在会话期间被管理员修改。
	cfg := s.set.Get()
	if !cfg.Allows(sess.Ext) {
		return nil, fmt.Errorf("%w: %s", ErrBadExt, displayExt(sess.Ext))
	}
	if sess.SizeBytes > cfg.MaxFileSizeBytes() {
		return nil, fmt.Errorf("%w: 上限为 %d MB", ErrTooLarge, cfg.MaxFileSizeMB)
	}

	chunks, err := s.store.ListChunks(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	if len(chunks) != sess.TotalChunks {
		return nil, fmt.Errorf("%w: 需要 %d 个分片，实际 %d 个", ErrIncomplete,
			sess.TotalChunks, len(chunks))
	}
	// 分片必须按序号 0..n-1 齐全，且磁盘文件真实存在。
	chunkRels := make([]string, sess.TotalChunks)
	var total int64
	for i, c := range chunks {
		if c.Idx != i {
			return nil, fmt.Errorf("%w: 缺少第 %d 个分片", ErrIncomplete, i)
		}
		if !s.st.Exists(c.RelPath) {
			return nil, fmt.Errorf("%w: 第 %d 个分片", ErrChunkBroken, c.Idx)
		}
		chunkRels[i] = c.RelPath
		total += c.SizeBytes
	}
	if total != sess.SizeBytes {
		return nil, fmt.Errorf("%w: 分片合计 %d 字节，声明 %d 字节", ErrSizeMatch, total, sess.SizeBytes)
	}
	if sess.SHA256 != "" {
		// 客户端声明了整文件 sha 时，先校验分片内容，避免无谓的合并。
		if err := s.verifyChunksSHA(ctx, chunkRels, sess.SHA256); err != nil {
			return nil, err
		}
	}

	// 先占位取得文件 ID，才能确定磁盘上的最终文件名（users/<owner>/<id><ext>）。
	placeholder := &model.File{
		OwnerID:     sess.OwnerID,
		OriginalNam: sess.OriginalName,
		Ext:         sess.Ext,
		SizeBytes:   sess.SizeBytes,
		Mime:        storage.MIMEFor(sess.Ext, sess.OriginalName),
		SHA256:      "",
		RelPath:     fmt.Sprintf("users/%d/pending-%s%s", sess.OwnerID, sess.ID, sess.Ext),
		Status:      model.StatusActive,
		ExpiresAt:   time.Now().UTC().Truncate(time.Second).AddDate(0, 0, cfg.RetentionDays),
	}
	if err := s.store.CreateFile(ctx, placeholder); err != nil {
		return nil, err
	}
	finalRel := storage.FileRel(sess.OwnerID, placeholder.ID, sess.Ext)

	// 合并写盘。
	size, sum, err := s.st.MergeTo(finalRel, chunkRels)
	if err != nil {
		// 合并失败：删除占位记录与可能的半成品，保持数据库与磁盘一致。
		_ = s.store.DeleteFileRow(ctx, placeholder.ID)
		_ = s.st.Remove(finalRel)
		return nil, err
	}

	// CAS 迁移会话状态：只有第一次成功的调用才继续写元数据。
	first, err := s.store.FinishUploadSession(ctx, sess.ID, placeholder.ID)
	if err != nil {
		_ = s.store.DeleteFileRow(ctx, placeholder.ID)
		_ = s.st.Remove(finalRel)
		return nil, err
	}
	if !first {
		// 并发重复请求：丢弃本次产物，返回另一个请求已生成的文件。
		_ = s.store.DeleteFileRow(ctx, placeholder.ID)
		_ = s.st.Remove(finalRel)
		if fresh, gerr := s.store.GetUploadSession(ctx, sess.ID); gerr == nil {
			if f, ferr := s.findCompletedFile(ctx, fresh); ferr == nil {
				return &CompleteResult{File: s.decorateFile(f, actor), Created: false, SHA256: f.SHA256}, nil
			}
		}
		return nil, fmt.Errorf("%w: 会话已完成", store.ErrState)
	}

	// 写最终元数据（rel_path 与 sha256）。失败则整体回滚。
	if err := s.store.FinishFile(ctx, placeholder.ID, finalRel, sum, size); err != nil {
		_ = s.store.DeleteFileRow(ctx, placeholder.ID)
		_ = s.st.Remove(finalRel)
		return nil, err
	}

	// 清理分片目录与分片记录；会话保留为 done，
	// 这样重复调用 complete 仍能幂等地返回同一个文件（24 小时后由维护任务回收会话）。
	_ = s.st.RemoveAll(storage.ChunkDirRel(sess.ID))
	if err := s.store.DeleteChunks(ctx, sess.ID); err != nil {
		// 清理失败不影响上传结果，交由维护任务兜底。
		_ = err
	}

	f, err := s.store.GetFile(ctx, placeholder.ID)
	if err != nil {
		return nil, err
	}
	return &CompleteResult{File: s.decorateFile(f, actor), Created: true, SHA256: sum}, nil
}

// Cancel 取消会话并删除分片。
func (s *Service) Cancel(ctx context.Context, id string, actor *model.User) error {
	sess, err := s.store.GetUploadSession(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrGone
		}
		return err
	}
	if err := s.checkOwner(sess, actor); err != nil {
		return err
	}
	if err := s.st.RemoveAll(storage.ChunkDirRel(sess.ID)); err != nil {
		return err
	}
	if sess.Status == model.UploadUploading {
		return s.store.AbortUploadSession(ctx, sess.ID)
	}
	return nil
}

// Abort 无条件清理会话（维护任务与错误恢复用）。
func (s *Service) Abort(ctx context.Context, sess *model.UploadSession) error {
	if err := s.st.RemoveAll(storage.ChunkDirRel(sess.ID)); err != nil {
		return err
	}
	return s.store.DeleteUploadSession(ctx, sess.ID)
}

// checkOwner 校验会话归属：只有会话属主本人（或管理员）可操作。
func (s *Service) checkOwner(sess *model.UploadSession, actor *model.User) error {
	if actor == nil {
		return ErrForbidden
	}
	if sess.OwnerID == actor.ID || actor.IsAdmin() {
		return nil
	}
	return fmt.Errorf("%w: 上传会话属于其他用户", ErrForbidden)
}

// decorate 填充 uploaded 分片列表。
func (s *Service) decorate(ctx context.Context, sess *model.UploadSession) (*model.UploadSession, error) {
	out := *sess
	chunks, err := s.store.ListChunks(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	out.Uploaded = make([]int, 0, len(chunks))
	for _, c := range chunks {
		out.Uploaded = append(out.Uploaded, c.Idx)
	}
	return &out, nil
}

// findCompletedFile 返回某个已完成会话产生的文件。
//
// 优先按会话记录的 file_id 精确查找；只有在旧数据缺失 file_id 时才回退到
// 「属主 + 文件名 + 大小」匹配（complete 的幂等返回用）。
func (s *Service) findCompletedFile(ctx context.Context, sess *model.UploadSession) (*model.File, error) {
	if sess.FileID != nil {
		return s.store.GetFile(ctx, *sess.FileID)
	}
	res, _, err := s.store.ListFiles(ctx, store.FileQuery{
		OwnerID:  sess.OwnerID,
		Status:   "all",
		Keyword:  sess.OriginalName,
		Sort:     "created_at",
		Order:    "desc",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		return nil, err
	}
	for i := range res {
		f := res[i]
		if f.OriginalNam == sess.OriginalName && f.SizeBytes == sess.SizeBytes {
			return &f, nil
		}
	}
	return nil, store.ErrNotFound
}

// verifyChunksSHA 在合并前按顺序流式校验分片内容的 sha256。
func (s *Service) verifyChunksSHA(ctx context.Context, chunkRels []string, want string) error {
	h, err := s.st.NewHasher()
	if err != nil {
		return err
	}
	for _, rel := range chunkRels {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.st.HashInto(h, rel); err != nil {
			return err
		}
	}
	got := h.Hex()
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%w: 客户端声明的 sha256 与实际内容不一致", store.ErrInvalidInput)
	}
	return nil
}

// TotalChunks 计算分片总数。0 字节文件也返回 1（有一个空分片），
// 保证 complete 阶段「分片数一致」的校验对所有文件都成立。
func TotalChunks(size, chunkSize int64) int {
	if chunkSize <= 0 {
		chunkSize = 4 << 20
	}
	if size <= 0 {
		return 1
	}
	total := int((size + chunkSize - 1) / chunkSize)
	if total < 1 {
		return 1
	}
	return total
}

// ChunkRange 返回第 idx 个分片在文件中的字节区间 [start, end)。
// 调用方必须先确认 idx 在 [0, TotalChunks) 内。
func ChunkRange(idx int, size, chunkSize int64) (start, end int64) {
	if chunkSize <= 0 {
		chunkSize = 4 << 20
	}
	start = int64(idx) * chunkSize
	end = start + chunkSize
	if end > size {
		end = size
	}
	if start > size {
		start = size
	}
	return start, end
}

// IsValidSHA256 报告字符串是否为合法的 64 位十六进制 sha256。
func IsValidSHA256(s string) bool { return normalizeSHA(s) != "" }

func normalizeSHA(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 64 {
		return ""
	}
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !ok {
			return ""
		}
	}
	return s
}

func bytesToMB(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return (n + (1 << 20) - 1) >> 20
}

func displayExt(ext string) string {
	if ext == "" {
		return "无扩展名文件"
	}
	return ext
}
