package settings

import (
	"context"
	"strings"
	"testing"
)

// memRepo 是 Repo 的内存实现，用于不依赖数据库地测试配置逻辑。
type memRepo struct {
	m       map[string]string
	failPut bool
}

func newMemRepo(init map[string]string) *memRepo {
	m := map[string]string{}
	for k, v := range init {
		m[k] = v
	}
	return &memRepo{m: m}
}

func (r *memRepo) AllSettings(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.m {
		out[k] = v
	}
	return out, nil
}

func (r *memRepo) PutSettings(_ context.Context, kv map[string]string) error {
	if r.failPut {
		return context.DeadlineExceeded
	}
	for k, v := range kv {
		r.m[k] = v
	}
	return nil
}

func TestDefaultsMatchRequirements(t *testing.T) {
	d := Defaults()
	// 需求：默认上传后 15 天删除，再保留 7 天。
	if d.RetentionDays != 15 {
		t.Fatalf("默认保留天数 = %d，需求为 15", d.RetentionDays)
	}
	if d.TrashDays != 7 {
		t.Fatalf("默认回收站天数 = %d，需求为 7", d.TrashDays)
	}
	// 需求：默认允许所有文件类型。
	if !d.AllowsAll() {
		t.Fatalf("默认应允许所有文件类型")
	}
	if !d.Allows(".exe") || !d.Allows(".any") {
		t.Fatalf("允许全部时任意扩展名都应通过")
	}
	if !d.UploadEnabled {
		t.Fatalf("默认应开启上传")
	}
	if d.MaxFileSizeMB != 500 || d.ChunkSizeMB != 4 {
		t.Fatalf("默认体积/分片 = %d/%d，期望 500/4", d.MaxFileSizeMB, d.ChunkSizeMB)
	}
	// 需求：可预览文件大小限制默认 20MB。
	if d.PreviewMaxSizeMB != 20 {
		t.Fatalf("默认预览上限 = %d MB，需求为 20", d.PreviewMaxSizeMB)
	}
	if d.PreviewMaxSizeBytes() != 20<<20 {
		t.Fatalf("PreviewMaxSizeBytes 换算错误: %d", d.PreviewMaxSizeBytes())
	}
	// 默认上限下：20MB 整可以预览，超出一字节就不行（边界不能"差不多"）。
	if !d.PreviewSizeAllowed(20 << 20) {
		t.Fatalf("恰好等于上限的文件应允许预览")
	}
	if d.PreviewSizeAllowed(20<<20 + 1) {
		t.Fatalf("超过上限一字节的文件不应允许预览")
	}
}

func TestNewLoadsStoredValues(t *testing.T) {
	repo := newMemRepo(map[string]string{
		KeyMaxFileSizeMB:     "1024",
		KeyRetentionDays:     "30",
		KeyTrashDays:         "3",
		KeyChunkSizeMB:       "8",
		KeyUploadEnabled:     "false",
		KeyAllowedExtensions: "pdf, docx",
	})
	svc, err := New(context.Background(), repo)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v := svc.Get()
	if v.MaxFileSizeMB != 1024 || v.RetentionDays != 30 || v.TrashDays != 3 || v.ChunkSizeMB != 8 {
		t.Fatalf("配置未正确载入: %+v", v)
	}
	if v.UploadEnabled {
		t.Fatalf("上传开关应为关闭")
	}
	if v.AllowsAll() {
		t.Fatalf("限制了类型时不应允许全部")
	}
	if !v.Allows(".pdf") || !v.Allows("PDF") || !v.Allows(".docx") {
		t.Fatalf("允许的扩展名应通过（大小写不敏感）")
	}
	if v.Allows(".exe") || v.Allows("") {
		t.Fatalf("未允许的扩展名应被拒绝")
	}
	if v.MaxFileSizeBytes() != 1024<<20 {
		t.Fatalf("MaxFileSizeBytes 换算错误")
	}
	if v.ChunkSizeBytes() != 8<<20 {
		t.Fatalf("ChunkSizeBytes 换算错误")
	}
}

func TestNewFallsBackOnInvalidStoredValues(t *testing.T) {
	repo := newMemRepo(map[string]string{
		KeyMaxFileSizeMB: "not-a-number",
		KeyRetentionDays: "-5",
		KeyTrashDays:     "99999",
	})
	svc, err := New(context.Background(), repo)
	if err != nil {
		t.Fatalf("非法存量配置不应导致启动失败: %v", err)
	}
	v := svc.Get()
	if v.MaxFileSizeMB != DefMaxFileSizeMB || v.RetentionDays != DefRetentionDays || v.TrashDays != DefTrashDays {
		t.Fatalf("非法值应回退到默认: %+v", v)
	}
}

func TestUpdateValidatesRanges(t *testing.T) {
	svc, _ := New(context.Background(), newMemRepo(nil))

	bad := []Patch{
		{MaxFileSizeMB: intPtr(0)},
		{MaxFileSizeMB: intPtr(MaxFileSizeMB + 1)},
		{RetentionDays: intPtr(0)},
		{RetentionDays: intPtr(MaxRetentionDay + 1)},
		{TrashDays: intPtr(-1)},
		{TrashDays: intPtr(MaxTrashDay + 1)},
		{ChunkSizeMB: intPtr(0)},
		{ChunkSizeMB: intPtr(MaxChunkSizeMB + 1)},
		{PreviewMaxSizeMB: intPtr(-1)},
		{PreviewMaxSizeMB: intPtr(MaxPreviewMaxSizeMB + 1)},
	}
	for i, p := range bad {
		if _, err := svc.Update(context.Background(), p); err == nil {
			t.Fatalf("非法配置 #%d 应被拒绝: %+v", i, p)
		}
	}
	// 合法边界值应通过。
	good := []Patch{
		{MaxFileSizeMB: intPtr(MinFileSizeMB)},
		{MaxFileSizeMB: intPtr(MaxFileSizeMB)},
		{RetentionDays: intPtr(MinRetentionDay)},
		{RetentionDays: intPtr(MaxRetentionDay)},
		{TrashDays: intPtr(MinTrashDay)},
		{TrashDays: intPtr(MaxTrashDay)},
		{ChunkSizeMB: intPtr(MinChunkSizeMB)},
		{ChunkSizeMB: intPtr(MaxChunkSizeMB)},
		// 0 是合法值：表示不限制预览体积。
		{PreviewMaxSizeMB: intPtr(MinPreviewMaxSizeMB)},
		{PreviewMaxSizeMB: intPtr(MaxPreviewMaxSizeMB)},
	}
	for i, p := range good {
		if _, err := svc.Update(context.Background(), p); err != nil {
			t.Fatalf("合法配置 #%d 应被接受: %v", i, err)
		}
	}
}

func TestUpdatePersistsAndRefreshesCache(t *testing.T) {
	repo := newMemRepo(nil)
	svc, _ := New(context.Background(), repo)

	size := 2048
	ret := 60
	trash := 10
	enabled := false
	exts := "PDF, .Docx, xlsx"
	previewMax := 64
	next, err := svc.Update(context.Background(), Patch{
		MaxFileSizeMB:     &size,
		RetentionDays:     &ret,
		TrashDays:         &trash,
		UploadEnabled:     &enabled,
		AllowedExtensions: &exts,
		PreviewMaxSizeMB:  &previewMax,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if next.MaxFileSizeMB != size || next.RetentionDays != ret || next.TrashDays != trash {
		t.Fatalf("返回值未更新: %+v", next)
	}
	if next.PreviewMaxSizeMB != previewMax {
		t.Fatalf("预览上限未更新: %+v", next)
	}
	if !next.PreviewSizeAllowed(64<<20) || next.PreviewSizeAllowed(64<<20+1) {
		t.Fatalf("预览上限边界判定错误")
	}
	if next.UploadEnabled {
		t.Fatalf("上传开关应已关闭")
	}
	// 扩展名应被规范化、排序、拼接。
	if next.AllowedExtensions != ".docx,.pdf,.xlsx" {
		t.Fatalf("扩展名规范化结果 = %q", next.AllowedExtensions)
	}
	// 缓存应立即生效。
	if got := svc.Get(); got.MaxFileSizeMB != size {
		t.Fatalf("缓存未刷新: %+v", got)
	}
	// 已落库，重新载入应保持一致。
	svc2, err := New(context.Background(), repo)
	if err != nil {
		t.Fatalf("重新载入: %v", err)
	}
	if svc2.Get().AllowedExtensions != ".docx,.pdf,.xlsx" {
		t.Fatalf("持久化后重新载入不一致: %+v", svc2.Get())
	}
	if svc2.Get().PreviewMaxSizeMB != previewMax {
		t.Fatalf("持久化后预览上限不一致: %+v", svc2.Get())
	}
}

// TestPreviewLimitZeroMeansUnlimited 守住"0 = 不限制"这条语义。
//
// 为什么值得单测：0 在这里**不是**"什么都不给预览"，而是"关掉这道闸"。
// 一旦被实现成"上限 0 字节"，管理员把值填 0 就会让所有文件都预览不了 ——
// 表面上配置成功、实际全坏，正是最该被测试挡住的那类反直觉取值。
func TestPreviewLimitZeroMeansUnlimited(t *testing.T) {
	svc, _ := New(context.Background(), newMemRepo(nil))

	zero := 0
	v, err := svc.Update(context.Background(), Patch{PreviewMaxSizeMB: &zero})
	if err != nil {
		t.Fatalf("0 应被接受（表示不限制）: %v", err)
	}
	if v.PreviewMaxSizeBytes() != 0 {
		t.Fatalf("上限 0 的字节数应为 0，实际 %d", v.PreviewMaxSizeBytes())
	}
	// 任意大的文件都应放行。
	for _, n := range []int64{1, 20 << 20, 1 << 40} {
		if !v.PreviewSizeAllowed(n) {
			t.Fatalf("上限为 0（不限制）时应允许 %d 字节的文件预览", n)
		}
	}
}

func TestUpdateEmptyExtensionsMeansAllowAll(t *testing.T) {
	svc, _ := New(context.Background(), newMemRepo(map[string]string{
		KeyAllowedExtensions: "pdf",
	}))
	empty := ""
	next, err := svc.Update(context.Background(), Patch{AllowedExtensions: &empty})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !next.AllowsAll() {
		t.Fatalf("清空扩展名应恢复为允许全部")
	}
	if !svc.Get().Allows(".exe") {
		t.Fatalf("允许全部后任意类型都应通过")
	}
}

func TestUpdateRejectsInvalidExtensions(t *testing.T) {
	svc, _ := New(context.Background(), newMemRepo(nil))
	bad := "pdf, !!invalid!!"
	if _, err := svc.Update(context.Background(), Patch{AllowedExtensions: &bad}); err == nil {
		t.Fatalf("非法扩展名应被拒绝")
	}
	// 带 * 前缀与多余符号的写法应被接受并规范化。
	ok := "*.PDF, JPG"
	next, err := svc.Update(context.Background(), Patch{AllowedExtensions: &ok})
	if err != nil {
		t.Fatalf("常见写法应被接受: %v", err)
	}
	if next.AllowedExtensions != ".jpg,.pdf" {
		t.Fatalf("规范化结果 = %q", next.AllowedExtensions)
	}
}

func TestUpdateFailureKeepsCacheIntact(t *testing.T) {
	repo := newMemRepo(nil)
	svc, _ := New(context.Background(), repo)
	before := svc.Get()

	repo.failPut = true
	size := 123
	if _, err := svc.Update(context.Background(), Patch{MaxFileSizeMB: &size}); err == nil {
		t.Fatalf("持久化失败时应返回错误")
	}
	if got := svc.Get(); got.MaxFileSizeMB != before.MaxFileSizeMB {
		t.Fatalf("持久化失败后缓存不应变化: %+v", got)
	}
}

func TestUpdateEmptyPatchIsNoop(t *testing.T) {
	repo := newMemRepo(nil)
	svc, _ := New(context.Background(), repo)
	if !(Patch{}).Empty() {
		t.Fatalf("空补丁应报告 Empty")
	}
	if (Patch{MaxFileSizeMB: intPtr(10)}).Empty() {
		t.Fatalf("非空补丁不应报告 Empty")
	}
	next, err := svc.Update(context.Background(), Patch{})
	if err != nil {
		t.Fatalf("空补丁不应报错: %v", err)
	}
	if next != svc.Get() {
		t.Fatalf("空补丁不应改变配置")
	}
}

func TestParseExtList(t *testing.T) {
	got := ParseExtList("pdf, .JPG；docx、xlsx  doc")
	want := []string{".doc", ".docx", ".jpg", ".pdf", ".xlsx"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ParseExtList = %v，期望 %v", got, want)
	}
	// 空串 = 允许全部
	if list := ParseExtList("   "); len(list) != 0 {
		t.Fatalf("空串应返回空列表，实际 %v", list)
	}
	// 宽松模式应过滤掉非法项而不是整体失败
	got2 := ParseExtList("pdf, %%bad%%, txt")
	if strings.Join(got2, ",") != ".pdf,.txt" {
		t.Fatalf("宽松解析应过滤非法项: %v", got2)
	}
	// 去重
	got3 := ParseExtList("pdf,pdf,PDF")
	if len(got3) != 1 || got3[0] != ".pdf" {
		t.Fatalf("应去重: %v", got3)
	}
}

func TestSeedOnlyFillsMissingKeys(t *testing.T) {
	repo := newMemRepo(map[string]string{
		KeyMaxFileSizeMB: "999", // 管理员已改过，不应被覆盖
	})
	if err := Seed(context.Background(), repo); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if repo.m[KeyMaxFileSizeMB] != "999" {
		t.Fatalf("已有配置被覆盖: %q", repo.m[KeyMaxFileSizeMB])
	}
	// 缺失的键应被补齐为默认值。
	if repo.m[KeyRetentionDays] != "15" {
		t.Fatalf("保留天数未播种: %q", repo.m[KeyRetentionDays])
	}
	if repo.m[KeyTrashDays] != "7" {
		t.Fatalf("回收站天数未播种: %q", repo.m[KeyTrashDays])
	}
	if _, ok := repo.m[KeyAllowedExtensions]; !ok {
		t.Fatalf("允许类型未播种（应为空串表示全部）")
	}
}

func intPtr(n int) *int { return &n }

// TestPermanentQuotaSemantics 守住永久配额的口径。
//
// 三个反直觉点，每个写错都会让"设为永久"要么拦不住、要么根本用不了：
//  1. 配额 0 = **关闭永久功能**（不是"只允许永久 0 字节"）；
//  2. 配额是**全站共享**的池，判定必须基于传入的 usedBytes；
//  3. 恰好等于配额要放行，超一字节才拒。
func TestPermanentQuotaSemantics(t *testing.T) {
	d := Defaults()
	// 需求：永久空间默认 100G。
	if d.PermanentQuotaMB != 102400 {
		t.Fatalf("默认永久配额 = %d MB，需求为 102400（100G）", d.PermanentQuotaMB)
	}
	if d.PermanentQuotaBytes() != 102400<<20 {
		t.Fatalf("PermanentQuotaBytes 换算错误: %d", d.PermanentQuotaBytes())
	}
	if !d.PermanentEnabled() {
		t.Fatalf("默认应开放永久功能")
	}

	quota := d.PermanentQuotaBytes()
	// 3) 边界：刚好放得下 / 超一字节放不下
	if !d.PermanentFits(0, quota) {
		t.Fatalf("空池应能放下恰好等于配额的文件")
	}
	if d.PermanentFits(0, quota+1) {
		t.Fatalf("超过配额一字节不应放行")
	}
	// 2) 已用要计入：剩 100 字节时，101 字节的文件放不下
	if !d.PermanentFits(quota-100, 100) {
		t.Fatalf("刚好用完剩余额度应放行")
	}
	if d.PermanentFits(quota-100, 101) {
		t.Fatalf("超出剩余额度应拒绝")
	}
	// 已用超过配额（管理员调低过）：任何新增都应被拒，且不能因减法溢出而误放行
	if d.PermanentFits(quota+1, 1) {
		t.Fatalf("已用超过配额时不应再放行")
	}

	// 1) 0 = 关闭
	off := Values{PermanentQuotaMB: 0}
	if off.PermanentEnabled() {
		t.Fatalf("配额为 0 应视为关闭永久功能")
	}
	if off.PermanentFits(0, 1) {
		t.Fatalf("永久功能关闭时任何文件都不该能设为永久")
	}
}
