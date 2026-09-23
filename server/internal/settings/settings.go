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
)

// 默认值。
const (
	DefMaxFileSizeMB = 500
	DefRetentionDays = 15
	DefTrashDays     = 7
	DefChunkSizeMB   = 4
	DefUploadEnabled = true
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
}

// MaxFileSizeBytes 单文件体积上限（字节）。
func (v Values) MaxFileSizeBytes() int64 { return int64(v.MaxFileSizeMB) << 20 }

// ChunkSizeBytes 分片大小（字节）。
func (v Values) ChunkSizeBytes() int64 { return int64(v.ChunkSizeMB) << 20 }

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
}

// Empty 报告该补丁是否未包含任何字段。
func (p Patch) Empty() bool {
	return p.MaxFileSizeMB == nil && p.AllowedExtensions == nil && p.RetentionDays == nil &&
		p.TrashDays == nil && p.ChunkSizeMB == nil && p.UploadEnabled == nil
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

	kv := map[string]string{
		KeyMaxFileSizeMB:     strconv.Itoa(next.MaxFileSizeMB),
		KeyAllowedExtensions: next.AllowedExtensions,
		KeyRetentionDays:     strconv.Itoa(next.RetentionDays),
		KeyTrashDays:         strconv.Itoa(next.TrashDays),
		KeyChunkSizeMB:       strconv.Itoa(next.ChunkSizeMB),
		KeyUploadEnabled:     strconv.FormatBool(next.UploadEnabled),
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
