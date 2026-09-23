// Package storage 负责磁盘与「相对数据根目录路径」之间的全部换算与安全校验。
//
// 核心约定（与 docs/design.md 一致）：
//   - 数据库中只保存相对数据根目录的路径，一律以 '/' 分隔；
//   - 任何来自数据库的相对路径在使用前都必须经过 SafeRel 校验；
//   - 磁盘文件名只用文件主键 + 规范化扩展名，原始文件名不进磁盘。
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// 领域错误。
var (
	ErrInvalidRel = errors.New("相对路径不合法")
	ErrNotFound   = errors.New("文件不存在")
)

// MaxRelLen 是数据库 rel_path 列（VARCHAR(512)）的长度上限。
const MaxRelLen = 512

// MaxNameLen 是数据库 original_name 列（VARCHAR(255)）的字节上限。
const MaxNameLen = 255

// Storage 以数据根目录为根的文件存储。
type Storage struct {
	root string // 绝对路径
}

// New 打开（必要时创建）数据根目录，并初始化 users 与 tmp/chunks 子目录。
func New(root string) (*Storage, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析数据根目录失败: %w", err)
	}
	for _, dir := range []string{abs, filepath.Join(abs, "users"), filepath.Join(abs, "tmp", "chunks")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录 %s 失败: %w", dir, err)
		}
	}
	return &Storage{root: abs}, nil
}

// Root 返回数据根目录的绝对路径。
func (s *Storage) Root() string { return s.root }

// SafeRel 校验并规范化一个相对路径。
//
// 只接受以 '/' 分隔、不含 "." / ".." / 空段 / 反斜杠 / 绝对前缀的相对路径。
// 返回值为清理后的规范形式（如 "users/12/34.pdf"）。
func SafeRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("%w: 空路径", ErrInvalidRel)
	}
	if len(rel) > MaxRelLen {
		return "", fmt.Errorf("%w: 路径过长（>%d 字节）", ErrInvalidRel, MaxRelLen)
	}
	if strings.ContainsRune(rel, '\\') {
		return "", fmt.Errorf("%w: 不允许反斜杠", ErrInvalidRel)
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: 不允许绝对路径", ErrInvalidRel)
	}
	if !utf8.ValidString(rel) {
		return "", fmt.Errorf("%w: 非法 UTF-8", ErrInvalidRel)
	}
	if strings.ContainsRune(rel, 0) {
		return "", fmt.Errorf("%w: 含空字节", ErrInvalidRel)
	}
	// 显式拒绝冒号：既覆盖 Windows 盘符（C:/...），也覆盖 NTFS 数据流（a.txt:evil）。
	// 本项目的相对路径只会由本包生成，因此这条限制没有副作用。
	if strings.ContainsRune(rel, ':') {
		return "", fmt.Errorf("%w: 不允许冒号（盘符或数据流）", ErrInvalidRel)
	}
	clean := path.Clean(rel)
	if clean != rel {
		return "", fmt.Errorf("%w: 非规范形式 %q", ErrInvalidRel, rel)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: 含非法路径段 %q", ErrInvalidRel, seg)
		}
	}
	// 双保险：Go 自带的本地路径判定。
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("%w: 不是本地相对路径", ErrInvalidRel)
	}
	return clean, nil
}

// Abs 把已校验的相对路径拼成绝对路径。
// 该路径必须来自 SafeRel 或本包生成的函数，绝不能是未经校验的用户输入。
func (s *Storage) Abs(rel string) (string, error) {
	clean, err := SafeRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.root, filepath.FromSlash(clean))
	// 再次确认结果仍在根目录内（防御符号链接与平台差异）。
	relCheck, err := filepath.Rel(s.root, full)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 逃逸出数据根目录", ErrInvalidRel)
	}
	return full, nil
}

// UserDirName 校验并返回用户目录名（即工号）。
//
// 目录名直接用工号而不是自增 id：数据落盘后看一眼目录就知道是谁的文件，
// 也便于运维按工号定位与备份。代价是**工号一旦确定就不能再改** ——
// 改了会让已有文件留在旧目录里成为孤儿，所以工号是账号的不可变标识
// （见 handler.UpdateUser：它的请求体不接受 employee_no）。
//
// 工号要当作路径段使用，因此这里做严格校验：只允许字母、数字、下划线、
// 连字符和点，且不能是 "." / ".." / 空串。比 handler 的宽松校验更严，
// 因为这是拼磁盘路径的最后一道关。
func UserDirName(employeeNo string) (string, error) {
	no := strings.TrimSpace(employeeNo)
	if no == "" {
		return "", fmt.Errorf("%w: 工号不能为空", ErrInvalidRel)
	}
	if no == "." || no == ".." {
		return "", fmt.Errorf("%w: 工号不能是 %q", ErrInvalidRel, no)
	}
	for _, r := range no {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return "", fmt.Errorf("%w: 工号含非法字符 %q（只允许字母、数字、_ - .）", ErrInvalidRel, r)
		}
	}
	// 交给 SafeRel 再确认一次：目录名必须是合法的单层路径段。
	if _, err := SafeRel("users/" + no); err != nil {
		return "", err
	}
	return no, nil
}

// UserDirRel 返回用户目录的相对路径（users/<工号>）。
// 入参必须是已通过 UserDirName 校验的工号。
func UserDirRel(employeeNo string) string { return "users/" + employeeNo }

// FileRel 返回文件最终存储的相对路径（<用户目录>/<fileID><ext>）。
//
// 入参是**用户目录**而不是 owner id：上传会话在创建时就把目标目录记了下来
// （upload_sessions.dir_rel），合并时沿用同一个值即可。这样存量数据
// （早期版本按 users/<id> 落盘）无需迁移也能继续正确写入自己的目录。
func FileRel(ownerDirRel string, fileID int64, ext string) string {
	return fmt.Sprintf("%s/%d%s", ownerDirRel, fileID, NormalizeExt(ext))
}

// ChunkRel 返回一个分片文件的相对路径（tmp/chunks/<uploadID>/<idx>.part）。
func ChunkRel(uploadID string, idx int) string {
	return fmt.Sprintf("tmp/chunks/%s/%d.part", uploadID, idx)
}

// ChunkDirRel 返回某个上传会话的分片目录。
func ChunkDirRel(uploadID string) string { return fmt.Sprintf("tmp/chunks/%s", uploadID) }

// NormalizeExt 规范化扩展名：转小写、补前导点、过滤非法字符。
// 无扩展名返回空串；超长时截断到 31 字节以内（ext 列为 VARCHAR(32)）。
func NormalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	var b strings.Builder
	for _, r := range ext {
		switch {
		case r == '.':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '+':
			b.WriteRune(r)
		default:
			// 丢弃其它字符（中文、空格、控制符等）
		}
	}
	out := b.String()
	if out == "." {
		return ""
	}
	if len(out) > 31 {
		out = out[:31]
	}
	return out
}

// ExtOf 从原始文件名取规范化扩展名。
func ExtOf(name string) string {
	// 只按最后一个点切分，且排除 ".bashrc" 这类纯前缀点文件。
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	i := strings.LastIndex(base, ".")
	if i <= 0 || i == len(base)-1 {
		return ""
	}
	return NormalizeExt(base[i:])
}

// SanitizeName 规范化原始文件名，使其可安全存储与展示。
//
//   - 去掉路径部分（用户可能提交 "C:\a\b.txt" 或 "../../x"）
//   - 去掉控制字符，去掉首尾空白与点
//   - 按字节上限截断，同时保证是合法 UTF-8
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "未命名文件"
	}
	if len(name) > MaxNameLen {
		// 按 rune 截断，避免切碎 UTF-8。
		for len(name) > MaxNameLen {
			_, size := utf8.DecodeLastRuneInString(name)
			name = name[:len(name)-size]
		}
		name = strings.TrimRight(name, " .")
		if name == "" {
			name = "未命名文件"
		}
	}
	return name
}

// SplitName 把文件名拆成主名与扩展名（含点）。
func SplitName(name string) (base, ext string) {
	ext = ExtOf(name)
	if ext == "" {
		return name, ""
	}
	return name[:len(name)-len(ext)], ext
}

// JoinName 拼接主名与扩展名。
func JoinName(base, ext string) string {
	if ext == "" {
		return base
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return base + ext
}

// EnsureUserDir 创建用户目录（users/<工号>），返回其相对路径。
func (s *Storage) EnsureUserDir(employeeNo string) (string, error) {
	name, err := UserDirName(employeeNo)
	if err != nil {
		return "", err
	}
	rel := UserDirRel(name)
	abs, err := s.Abs(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("创建用户目录失败: %w", err)
	}
	return rel, nil
}

// EnsureParent 为某个相对路径创建父目录。
func (s *Storage) EnsureParent(rel string) error {
	abs, err := s.Abs(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(abs), 0o755)
}

// WriteChunk 把一个分片写到目标相对路径（幂等覆盖，先写临时文件再改名）。
func (s *Storage) WriteChunk(rel string, r io.Reader, limit int64) (int64, error) {
	abs, err := s.Abs(rel)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, err
	}
	tmp := abs + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	// limit 为分片大小上限 + 1，用于探测超长请求。
	written, err := io.Copy(f, io.LimitReader(r, limit))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return written, err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return written, err
	}
	return written, nil
}

// Exists 报告相对路径对应的文件是否存在。
func (s *Storage) Exists(rel string) bool {
	abs, err := s.Abs(rel)
	if err != nil {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && st.Mode().IsRegular()
}

// Remove 删除相对路径对应的文件；文件不存在视为成功。
func (s *Storage) Remove(rel string) error {
	abs, err := s.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveAll 递归删除相对路径对应的目录/文件；不存在视为成功。
func (s *Storage) RemoveAll(rel string) error {
	abs, err := s.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MergeTo 按顺序把分片合并成最终文件，并同时计算 sha256。
//
// 合并写入 <rel>.merging 临时文件，成功后原子改名为 rel，
// 因此中途失败不会留下看似有效的半成品文件。
func (s *Storage) MergeTo(rel string, chunkRels []string) (size int64, sum string, err error) {
	abs, err := s.Abs(rel)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, "", err
	}
	tmp := abs + ".merging"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, "", err
	}
	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	cleanup := func() {
		_ = out.Close()
		_ = os.Remove(tmp)
	}
	for _, cr := range chunkRels {
		cabs, err := s.Abs(cr)
		if err != nil {
			cleanup()
			return 0, "", err
		}
		in, err := os.Open(cabs)
		if err != nil {
			cleanup()
			return 0, "", fmt.Errorf("读取分片 %s 失败: %w", cr, err)
		}
		n, err := io.Copy(writer, in)
		_ = in.Close()
		if err != nil {
			cleanup()
			return 0, "", fmt.Errorf("合并分片 %s 失败: %w", cr, err)
		}
		size += n
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return 0, "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

// HashFile 计算相对路径对应文件的 sha256 与大小。
func (s *Storage) HashFile(rel string) (int64, string, error) {
	abs, err := s.Abs(rel)
	if err != nil {
		return 0, "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes 计算字节切片的 sha256（十六进制）。
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// WalkFiles 遍历数据根目录下所有普通文件，回调收到相对路径与信息。
// relPrefix 为空的遍历范围（如 "users"），用于一致性扫描。
func (s *Storage) WalkFiles(relPrefix string, fn func(rel string, info os.FileInfo) error) error {
	start := s.root
	if relPrefix != "" {
		clean, err := SafeRel(relPrefix)
		if err != nil {
			return err
		}
		start = filepath.Join(s.root, filepath.FromSlash(clean))
	}
	if _, err := os.Stat(start); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(start, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// 单个条目读取失败不应中断整体扫描。
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(s.root, p)
		if rerr != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		return fn(filepath.ToSlash(rel), info)
	})
}

// DirSize 统计某个相对目录下的文件数与总字节。
func (s *Storage) DirSize(relDir string) (count int64, bytes int64, err error) {
	err = s.WalkFiles(relDir, func(_ string, info os.FileInfo) error {
		count++
		bytes += info.Size()
		return nil
	})
	return count, bytes, err
}

// Hasher 是流式 sha256 计算器，用于按序校验分片内容。
type Hasher struct{ h hash.Hash }

// NewHasher 创建 sha256 计算器。
func (s *Storage) NewHasher() (*Hasher, error) { return &Hasher{h: sha256.New()}, nil }

// HashInto 把一个相对路径对应的文件内容追加进计算器。
func (s *Storage) HashInto(h *Hasher, rel string) error {
	abs, err := s.Abs(rel)
	if err != nil {
		return err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrNotFound, rel)
		}
		return err
	}
	defer f.Close()
	_, err = io.Copy(h.h, f)
	return err
}

// Hex 返回十六进制校验值。
func (h *Hasher) Hex() string { return hex.EncodeToString(h.h.Sum(nil)) }

// Sum 返回原始校验字节。
func (h *Hasher) Sum() []byte { return h.h.Sum(nil) }

// Write 向计算器写入字节。
func (h *Hasher) Write(p []byte) (int, error) { return h.h.Write(p) }

// PruneEmptyDirs 自底向上删除空的相对目录（不会删除数据根目录本身）。
// 用于文件被删除后清理 users/<id> 这类空目录。
func (s *Storage) PruneEmptyDirs(relDir string) error {
	clean, err := SafeRel(relDir)
	if err != nil {
		return err
	}
	for clean != "" && clean != "." {
		abs, err := s.Abs(clean)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}
			return err
		}
		if len(entries) > 0 {
			break
		}
		if err := os.Remove(abs); err != nil {
			// 目录非空（并发写入）或权限不足时停止向上清理。
			break
		}
		idx := strings.LastIndex(clean, "/")
		if idx < 0 {
			break
		}
		clean = clean[:idx]
	}
	return nil
}

// Move 把相对路径 from 的文件移动到相对路径 to（用于孤儿文件归档）。
func (s *Storage) Move(from, to string) error {
	fabs, err := s.Abs(from)
	if err != nil {
		return err
	}
	tabs, err := s.Abs(to)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tabs), 0o755); err != nil {
		return err
	}
	return os.Rename(fabs, tabs)
}

// extMIME 是扩展名 → MIME 的映射表，只覆盖常见类型；
// 未知扩展名一律回退 application/octet-stream（服务端绝不信任用户输入决定内容类型）。
var extMIME = map[string]string{
	".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8",
	".csv": "text/csv; charset=utf-8", ".log": "text/plain; charset=utf-8",
	".json": "application/json", ".xml": "application/xml", ".yml": "text/yaml; charset=utf-8",
	".yaml": "text/yaml; charset=utf-8", ".ini": "text/plain; charset=utf-8",
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".ts": "text/plain; charset=utf-8", ".go": "text/plain; charset=utf-8",
	".py": "text/plain; charset=utf-8", ".sh": "text/plain; charset=utf-8",
	".java": "text/plain; charset=utf-8", ".c": "text/plain; charset=utf-8",
	".cpp": "text/plain; charset=utf-8", ".sql": "text/plain; charset=utf-8",
	".pdf": "application/pdf",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	".svg": "image/svg+xml", ".ico": "image/x-icon", ".tif": "image/tiff", ".tiff": "image/tiff",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".m4a": "audio/mp4", ".ogg": "audio/ogg",
	".flac": "audio/flac", ".aac": "audio/aac",
	".mp4": "video/mp4", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".mov": "video/quicktime", ".avi": "video/x-msvideo",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".zip":  "application/zip", ".rar": "application/vnd.rar", ".7z": "application/x-7z-compressed",
	".gz": "application/gzip", ".tar": "application/x-tar",
}

// MIMEFor 返回扩展名对应的 MIME；未知类型回退 application/octet-stream。
func MIMEFor(ext, name string) string {
	e := NormalizeExt(ext)
	if e == "" {
		e = ExtOf(name)
	}
	if m, ok := extMIME[e]; ok {
		return m
	}
	return "application/octet-stream"
}

// IsInlinePreviewable 报告该扩展名是否可以安全地内联展示（浏览器直接渲染）。
// 文本类会以纯文本 + 转义方式展示；HTML/SVG 不内联，避免 XSS 与脚本执行。
func IsInlinePreviewable(ext string) bool {
	e := NormalizeExt(ext)
	switch e {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".tif", ".tiff",
		".pdf", ".mp4", ".webm", ".mov", ".ogg",
		".mp3", ".wav", ".m4a", ".flac", ".aac":
		return true
	}
	return false
}

// IsText 报告该扩展名是否按纯文本展示。
func IsText(ext string) bool {
	switch NormalizeExt(ext) {
	case ".txt", ".md", ".csv", ".log", ".json", ".xml", ".yml", ".yaml", ".ini",
		".js", ".ts", ".go", ".py", ".sh", ".java", ".c", ".cpp", ".sql", ".conf", ".env":
		return true
	}
	return false
}
