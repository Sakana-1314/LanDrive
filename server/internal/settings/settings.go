// Package settings 提供系统配置的读写、校验与内存缓存。
//
// 配置项存于 settings 表（key/value），启动时载入缓存，
// 管理员更新后立即刷新，读取路径全部走内存，避免每次请求打库。
package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 配置键。
const (
	KeyMaxFileSizeMB     = "max_file_size_mb"
	KeyAllowedExtensions = "allowed_extensions"
	KeyRetentionDays     = "retention_days"
	KeyTrashDays         = "trash_days"
	KeyChunkSizeMB       = "chunk_size_mb"
	KeyUploadEnabled     = "upload_enabled"
	KeyPreviewMaxSizeMB  = "preview_max_size_mb"
	KeyPermanentQuotaMB  = "permanent_quota_mb"
)

// 默认值。
const (
	DefMaxFileSizeMB    = 500
	DefRetentionDays    = 15
	DefTrashDays        = 7
	DefChunkSizeMB      = 4
	DefUploadEnabled    = true
	DefPreviewMaxSizeMB = 20
	// 永久空间默认 100GB。这是**全站共享**的一个池（见 PermanentQuotaMB 的说明）。
	DefPermanentQuotaMB = 102400
)

// 取值边界。
const (
	MinFileSizeMB   = 1
	MaxFileSizeMB   = 102400 // 100 GB
	MinRetentionDay = 1
	MaxRetentionDay = 3650
	MinTrashDay     = 0
	MaxTrashDay     = 365
	MinChunkSizeMB  = 1
	MaxChunkSizeMB  = 64
	// 预览上限允许 0：表示"不限制预览体积"，
	// 有人确实愿意让浏览器去扛大文件，给一个明确的关闭档比逼着他填个大数好。
	MinPreviewMaxSizeMB = 0
	MaxPreviewMaxSizeMB = 102400 // 100 GB
	// 永久配额允许 0：表示**关闭**"设为永久"这个功能（一律按保留天数到期）。
	// 这比填个极小的值更明确 —— 填 1MB 会让人以为是"额度很小"，
	// 而 0 表达的是"不打算给永久空间"。
	MinPermanentQuotaMB = 0
	MaxPermanentQuotaMB = 104857600 // 100 TB
)

// SettingsErr 表示配置校验失败（handler 映射为 400）。
var ErrInvalid = errors.New("配置不合法")

// Repo 是 settings 包需要的最小存储接口（便于单测注入）。
type Repo interface {
	AllSettings(ctx context.Context) (map[string]string, error)
	PutSettings(ctx context.Context, kv map[string]string) error
}

// Values 是生效中的配置快照。
type Values struct {
	MaxFileSizeMB     int    `json:"max_file_size_mb"`
	AllowedExtensions string `json:"allowed_extensions"` // 空串表示允许全部
	RetentionDays     int    `json:"retention_days"`
	TrashDays         int    `json:"trash_days"`
	ChunkSizeMB       int    `json:"chunk_size_mb"`
	UploadEnabled     bool   `json:"upload_enabled"`
	// PreviewMaxSizeMB 是**在线预览**的体积上限（MB）。0 表示不限制。
	//
	// 与 MaxFileSizeMB 是两件事：前者约束"能不能传进来"，这个约束
	// "要不要在浏览器里渲染"。预览要把整个文件读进内存（docx/xlsx/pptx 还要
	// 交给纯前端库解析），远超体积的文件会把标签页拖死，因此单独设一道闸；
	// **下载不受它影响** —— 大文件下下来本地看完全没问题。
	PreviewMaxSizeMB int `json:"preview_max_size_mb"`
	// PermanentQuotaMB 是**全站共享**的「永久文件」体积上限（MB）。0 表示关闭永久功能。
	//
	// 为什么是全站一个池而不是每人一份：永久文件永远不会被自动清理，
	// 每人一份的话"总量"随人数无限增长，磁盘迟早被撑爆 —— 而这道闸的意义
	// 正是给"永不清理"这件事封一个可预期的上限。全站共享让实际占用一眼可见。
	//
	// 计数口径：所有 **active 且 expires_at IS NULL** 的文件的 size_bytes 之和。
	// 刻意**不**把回收站里的永久文件算进来：那些已经设了 purge_at、几天内必被清掉，
	// 而"删掉文件却不释放额度"会让用户完全无法自救（这正是额度满时唯一的出路）。
	// 代价是存在一个有限的放大：删掉的永久文件在回收站里仍占几天磁盘，
	// 期间可以再设新的永久文件。放大倍数受 trash_days 约束、不会无限增长。
	PermanentQuotaMB int `json:"permanent_quota_mb"`
}

// MaxFileSizeBytes 单文件体积上限（字节）。
func (v Values) MaxFileSizeBytes() int64 { return int64(v.MaxFileSizeMB) << 20 }

// ChunkSizeBytes 分片大小（字节）。
func (v Values) ChunkSizeBytes() int64 { return int64(v.ChunkSizeMB) << 20 }

// PreviewMaxSizeBytes 预览体积上限（字节）。0 表示不限制。
func (v Values) PreviewMaxSizeBytes() int64 { return int64(v.PreviewMaxSizeMB) << 20 }

// PreviewSizeAllowed 报告某个体积的文件是否允许在线预览。
// 上限配成 0 时视为不限制（管理员显式关闭了这道闸）。
func (v Values) PreviewSizeAllowed(sizeBytes int64) bool {
	max := v.PreviewMaxSizeBytes()
	return max <= 0 || sizeBytes <= max
}

// PermanentQuotaBytes 永久空间上限（字节）。0 表示永久功能被关闭。
func (v Values) PermanentQuotaBytes() int64 { return int64(v.PermanentQuotaMB) << 20 }

// PermanentEnabled 报告是否开放「设为永久」。配额配成 0 即关闭该功能。
func (v Values) PermanentEnabled() bool { return v.PermanentQuotaMB > 0 }

// PermanentFits 报告在已用 usedBytes 的基础上再永久化 addBytes 是否放得下。
// 配额关闭（0）时恒为 false —— 关闭状态下没有任何文件能被设为永久。
//
// 用减法而不是加法比较，避免 usedBytes+addBytes 在极端值下溢出 int64。
func (v Values) PermanentFits(usedBytes, addBytes int64) bool {
	if !v.PermanentEnabled() {
		return false
	}
	quota := v.PermanentQuotaBytes()
	if usedBytes > quota {
		return false
	}
	return addBytes <= quota-usedBytes
}

// ExtList 返回规范化后的允许扩展名列表（小写、带点、去重、已排序）。
// 空列表表示允许全部。
func (v Values) ExtList() []string { return ParseExtList(v.AllowedExtensions) }

// AllowsAll 报告是否允许全部文件类型。
func (v Values) AllowsAll() bool { return len(v.ExtList()) == 0 }

// Allows 报告某个扩展名是否被允许。空列表 = 允许全部。
func (v Values) Allows(ext string) bool {
	list := v.ExtList()
	if len(list) == 0 {
		return true
	}
	ext = normalizeOneExt(ext)
	if ext == "" {
		return false
	}
	for _, e := range list {
		if e == ext {
			return true
		}
	}
	return false
}

// Service 配置服务（带读写锁的内存缓存）。
type Service struct {
	repo Repo

	mu     sync.RWMutex
	values Values
}

// New 从 repo 载入配置并构造服务。缺失的键使用默认值。
func New(ctx context.Context, repo Repo) (*Service, error) {
	s := &Service{repo: repo}
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload 从数据库重新载入配置。
func (s *Service) Reload(ctx context.Context) error {
	raw, err := s.repo.AllSettings(ctx)
	if err != nil {
		return err
	}
	v, err := fromMap(raw)
	if err != nil {
		// 数据库中若存在非法值，回退到默认值而不是让服务起不来。
		v, _ = fromMap(nil)
	}
	s.mu.Lock()
	s.values = v
	s.mu.Unlock()
	return nil
}

// Get 返回当前配置快照（值拷贝，可安全并发使用）。
func (s *Service) Get() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values
}

// Patch 描述一次部分更新，nil 表示不改该字段。
type Patch struct {
	MaxFileSizeMB     *int
	AllowedExtensions *string
	RetentionDays     *int
	TrashDays         *int
	ChunkSizeMB       *int
	UploadEnabled     *bool
	PreviewMaxSizeMB  *int
	PermanentQuotaMB  *int
}

// Empty 报告该补丁是否未包含任何字段。
func (p Patch) Empty() bool {
	return p.MaxFileSizeMB == nil && p.AllowedExtensions == nil && p.RetentionDays == nil &&
		p.TrashDays == nil && p.ChunkSizeMB == nil && p.UploadEnabled == nil &&
		p.PreviewMaxSizeMB == nil && p.PermanentQuotaMB == nil
}

// Update 校验并持久化补丁，成功后立即刷新缓存。返回更新后的配置。
func (s *Service) Update(ctx context.Context, p Patch) (Values, error) {
	cur := s.Get()
	next := cur

	if p.MaxFileSizeMB != nil {
		if err := checkRange("单文件最大体积(MB)", *p.MaxFileSizeMB, MinFileSizeMB, MaxFileSizeMB); err != nil {
			return cur, err
		}
		next.MaxFileSizeMB = *p.MaxFileSizeMB
	}
	if p.RetentionDays != nil {
		if err := checkRange("保留天数", *p.RetentionDays, MinRetentionDay, MaxRetentionDay); err != nil {
			return cur, err
		}
		next.RetentionDays = *p.RetentionDays
	}
	if p.TrashDays != nil {
		if err := checkRange("回收站保留天数", *p.TrashDays, MinTrashDay, MaxTrashDay); err != nil {
			return cur, err
		}
		next.TrashDays = *p.TrashDays
	}
	if p.ChunkSizeMB != nil {
		if err := checkRange("分片大小(MB)", *p.ChunkSizeMB, MinChunkSizeMB, MaxChunkSizeMB); err != nil {
			return cur, err
		}
		next.ChunkSizeMB = *p.ChunkSizeMB
	}
	if p.AllowedExtensions != nil {
		raw := *p.AllowedExtensions
		if _, err := parseExtListStrict(raw); err != nil {
			return cur, err
		}
		next.AllowedExtensions = joinExtList(ParseExtList(raw))
	}
	if p.UploadEnabled != nil {
		next.UploadEnabled = *p.UploadEnabled
	}
	if p.PreviewMaxSizeMB != nil {
		if err := checkRange("可预览文件上限(MB)", *p.PreviewMaxSizeMB, MinPreviewMaxSizeMB, MaxPreviewMaxSizeMB); err != nil {
			return cur, err
		}
		next.PreviewMaxSizeMB = *p.PreviewMaxSizeMB
	}
	if p.PermanentQuotaMB != nil {
		if err := checkRange("永久空间配额(MB)", *p.PermanentQuotaMB, MinPermanentQuotaMB, MaxPermanentQuotaMB); err != nil {
			return cur, err
		}
		// 允许把配额调到**低于**当前已用：已经设成永久的文件不动
		// （反过来把用户已获得的东西悄悄收回，比"暂时不能再设"严重得多），
		// 只是在新设永久时会因为超额度而被拒。这是有意的语义，写在这里备查。
		next.PermanentQuotaMB = *p.PermanentQuotaMB
	}

	kv := map[string]string{
		KeyMaxFileSizeMB:     strconv.Itoa(next.MaxFileSizeMB),
		KeyAllowedExtensions: next.AllowedExtensions,
		KeyRetentionDays:     strconv.Itoa(next.RetentionDays),
		KeyTrashDays:         strconv.Itoa(next.TrashDays),
		KeyChunkSizeMB:       strconv.Itoa(next.ChunkSizeMB),
		KeyUploadEnabled:     strconv.FormatBool(next.UploadEnabled),
		KeyPreviewMaxSizeMB:  strconv.Itoa(next.PreviewMaxSizeMB),
		KeyPermanentQuotaMB:  strconv.Itoa(next.PermanentQuotaMB),
	}
	if err := s.repo.PutSettings(ctx, kv); err != nil {
		return cur, err
	}

	s.mu.Lock()
	s.values = next
	s.mu.Unlock()
	return next, nil
}

// Defaults 返回默认配置（种子数据与测试使用）。
func Defaults() Values {
	return Values{
		MaxFileSizeMB:     DefMaxFileSizeMB,
		AllowedExtensions: "",
		RetentionDays:     DefRetentionDays,
		TrashDays:         DefTrashDays,
		ChunkSizeMB:       DefChunkSizeMB,
		UploadEnabled:     DefUploadEnabled,
		PreviewMaxSizeMB:  DefPreviewMaxSizeMB,
		PermanentQuotaMB:  DefPermanentQuotaMB,
	}
}

// fromMap 把数据库中的原始键值解析成 Values。非法值回退到默认值。
func fromMap(raw map[string]string) (Values, error) {
	v := Defaults()
	var errs []string

	if s, ok := raw[KeyMaxFileSizeMB]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinFileSizeMB || n > MaxFileSizeMB {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyMaxFileSizeMB, s))
		} else {
			v.MaxFileSizeMB = n
		}
	}
	if s, ok := raw[KeyRetentionDays]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinRetentionDay || n > MaxRetentionDay {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyRetentionDays, s))
		} else {
			v.RetentionDays = n
		}
	}
	if s, ok := raw[KeyTrashDays]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinTrashDay || n > MaxTrashDay {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyTrashDays, s))
		} else {
			v.TrashDays = n
		}
	}
	if s, ok := raw[KeyChunkSizeMB]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinChunkSizeMB || n > MaxChunkSizeMB {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyChunkSizeMB, s))
		} else {
			v.ChunkSizeMB = n
		}
	}
	if s, ok := raw[KeyUploadEnabled]; ok {
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyUploadEnabled, s))
		} else {
			v.UploadEnabled = b
		}
	}
	if s, ok := raw[KeyAllowedExtensions]; ok {
		list, err := parseExtListStrict(s)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			v.AllowedExtensions = joinExtList(list)
		}
	}
	if s, ok := raw[KeyPreviewMaxSizeMB]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinPreviewMaxSizeMB || n > MaxPreviewMaxSizeMB {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyPreviewMaxSizeMB, s))
		} else {
			v.PreviewMaxSizeMB = n
		}
	}
	if s, ok := raw[KeyPermanentQuotaMB]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < MinPermanentQuotaMB || n > MaxPermanentQuotaMB {
			errs = append(errs, fmt.Sprintf("%s=%q 非法", KeyPermanentQuotaMB, s))
		} else {
			v.PermanentQuotaMB = n
		}
	}
	if len(errs) > 0 {
		return v, fmt.Errorf("%w: %s", ErrInvalid, strings.Join(errs, "；"))
	}
	return v, nil
}

// Seed 返回首次启动需要写入的默认配置键值（仅补齐缺失项）。
func Seed(ctx context.Context, repo Repo) error {
	raw, err := repo.AllSettings(ctx)
	if err != nil {
		return err
	}
	kv := map[string]string{}
	d := Defaults()
	add := func(k, v string) {
		if _, ok := raw[k]; !ok {
			kv[k] = v
		}
	}
	add(KeyMaxFileSizeMB, strconv.Itoa(d.MaxFileSizeMB))
	add(KeyAllowedExtensions, d.AllowedExtensions)
	add(KeyRetentionDays, strconv.Itoa(d.RetentionDays))
	add(KeyTrashDays, strconv.Itoa(d.TrashDays))
	add(KeyChunkSizeMB, strconv.Itoa(d.ChunkSizeMB))
	add(KeyUploadEnabled, strconv.FormatBool(d.UploadEnabled))
	add(KeyPreviewMaxSizeMB, strconv.Itoa(d.PreviewMaxSizeMB))
	add(KeyPermanentQuotaMB, strconv.Itoa(d.PermanentQuotaMB))
	return repo.PutSettings(ctx, kv)
}

// ParseExtList 宽松解析：接受 "pdf,jpg"、" .PDF ， jpg "、"pdf jpg"、"pdf;jpg"。
// 忽略空项与非法项，返回排序去重后的规范列表。
func ParseExtList(s string) []string {
	list, err := parseExtListStrict(s)
	if err != nil {
		// 宽松模式：逐项过滤非法项。
		fields := strings.FieldsFunc(s, isExtSeparator)
		out := map[string]bool{}
		for _, f := range fields {
			if e := normalizeOneExt(f); e != "" {
				out[e] = true
			}
		}
		return sortedKeys(out)
	}
	return list
}

// parseExtListStrict 严格解析：任一项非法即报错（供配置更新校验使用）。
func parseExtListStrict(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil // 空 = 允许全部
	}
	fields := strings.FieldsFunc(s, isExtSeparator)
	out := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		e := normalizeOneExt(f)
		if e == "" || len(e) < 2 || len(e) > 31 {
			return nil, fmt.Errorf("%w: 非法扩展名 %q", ErrInvalid, f)
		}
		out[e] = true
	}
	if len(out) == 0 {
		return nil, nil
	}
	return sortedKeys(out), nil
}

// isExtSeparator 报告某个字符是否用于分隔扩展名。
// 同时接受中英文标点，避免用户粘贴中文逗号/分号时整串被当成一个非法扩展名。
func isExtSeparator(r rune) bool {
	switch r {
	case ',', ';', '\t', '\n', '\r', ' ',
		'\uFF0C', // ，
		'\uFF1B', // ；
		'\u3001', // 、
		'\u3002', // 。
		'\uFF5E': // ～
		return true
	}
	return false
}

// normalizeOneExt 把 "PDF"、".PDF"、"*.pdf"、"pdf" 统一成 ".pdf"。
// 含非法字符时返回空串。
func normalizeOneExt(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimPrefix(s, ".")
	s = strings.ToLower(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '+'
		if !ok {
			return ""
		}
	}
	return "." + s
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinExtList(list []string) string { return strings.Join(list, ",") }

func checkRange(label string, v, min, max int) error {
	if v < min || v > max {
		return fmt.Errorf("%w: %s 必须在 %d 到 %d 之间，当前 %d", ErrInvalid, label, min, max, v)
	}
	return nil
}

// Now 便于测试注入时间。
var Now = func() time.Time { return time.Now().UTC() }
