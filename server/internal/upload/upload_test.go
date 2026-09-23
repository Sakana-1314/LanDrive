package upload

import (
	"context"
	"testing"

	"lan-drive/internal/model"
	"lan-drive/internal/settings"
)

// 本文件只覆盖不依赖数据库的纯逻辑：分片数学、sha256 校验、权限判定与错误语义。

func TestTotalChunks(t *testing.T) {
	const mb = int64(1) << 20
	cases := []struct {
		size      int64
		chunkSize int64
		want      int
	}{
		{0, 4 * mb, 1},        // 0 字节文件也必须有一个空分片
		{1, 4 * mb, 1},        // 不足一片
		{4 * mb, 4 * mb, 1},   // 正好一片
		{4*mb + 1, 4 * mb, 2}, // 略超一片
		{8 * mb, 4 * mb, 2},   // 正好两片
		{10 * mb, 4 * mb, 3},  // 三片
		{100, 0, 1},           // 非法分片大小回退默认
		{-5, 4 * mb, 1},       // 负数按空文件处理
	}
	for _, c := range cases {
		if got := TotalChunks(c.size, c.chunkSize); got != c.want {
			t.Fatalf("TotalChunks(%d, %d) = %d，期望 %d", c.size, c.chunkSize, got, c.want)
		}
	}
}

func TestChunkRangeCoversFileExactly(t *testing.T) {
	const chunk = int64(1000)
	sizes := []int64{0, 1, 999, 1000, 1001, 2500, 10000}
	for _, size := range sizes {
		total := TotalChunks(size, chunk)
		var covered int64
		for i := 0; i < total; i++ {
			start, end := ChunkRange(i, size, chunk)
			if start > end {
				t.Fatalf("size=%d idx=%d 区间非法: [%d,%d)", size, i, start, end)
			}
			if start < 0 || end > size {
				t.Fatalf("size=%d idx=%d 区间越界: [%d,%d)", size, i, start, end)
			}
			covered += end - start
		}
		if covered != size {
			t.Fatalf("size=%d 分片总覆盖 %d 字节，不等于文件大小", size, covered)
		}
	}
	// 中间分片应完整
	start, end := ChunkRange(1, 2500, chunk)
	if start != 1000 || end != 2000 {
		t.Fatalf("中间分片区间 = [%d,%d)，期望 [1000,2000)", start, end)
	}
	// 最后一个分片应截断到文件末尾
	start, end = ChunkRange(2, 2500, chunk)
	if start != 2000 || end != 2500 {
		t.Fatalf("末分片区间 = [%d,%d)，期望 [2000,2500)", start, end)
	}
}

func TestNormalizeSHAAndValidation(t *testing.T) {
	valid := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if normalizeSHA(valid) != valid {
		t.Fatalf("合法 sha256 应原样返回")
	}
	// 大写会被规范化
	upper := "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"
	if normalizeSHA(upper) != valid {
		t.Fatalf("大写 sha256 应被规范化为小写")
	}
	// 带空格
	if normalizeSHA("  "+valid+"  ") != valid {
		t.Fatalf("应去除首尾空白")
	}
	bad := []string{
		"",
		"abc",
		valid + "a", // 65 位
		valid[:63],  // 63 位
		"z94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9", // 非十六进制
	}
	for _, b := range bad {
		if normalizeSHA(b) != "" {
			t.Fatalf("非法 sha256 %q 应返回空串", b)
		}
	}
	if !IsValidSHA256(valid) || IsValidSHA256("nope") {
		t.Fatalf("IsValidSHA256 判定错误")
	}
}

func TestBytesToMB(t *testing.T) {
	const mb = int64(1) << 20
	cases := map[int64]int64{
		0:        0,
		-1:       0,
		1:        1, // 向上取整
		mb:       1,
		mb + 1:   2,
		2 * mb:   2,
		500 * mb: 500,
	}
	for in, want := range cases {
		if got := bytesToMB(in); got != want {
			t.Fatalf("bytesToMB(%d) = %d，期望 %d", in, got, want)
		}
	}
}

func TestDisplayExt(t *testing.T) {
	if displayExt("") != "无扩展名文件" {
		t.Fatalf("空扩展名应有可读提示")
	}
	if displayExt(".pdf") != ".pdf" {
		t.Fatalf("displayExt 应原样返回扩展名")
	}
}

func TestCheckOwner(t *testing.T) {
	svc := &Service{}
	owner := &model.User{ID: 7, Role: model.RoleUser}
	other := &model.User{ID: 8, Role: model.RoleUser}
	admin := &model.User{ID: 9, Role: model.RoleAdmin}
	sess := &model.UploadSession{ID: "abc", OwnerID: 7}

	if err := svc.checkOwner(sess, owner); err != nil {
		t.Fatalf("属主应可操作: %v", err)
	}
	if err := svc.checkOwner(sess, other); err == nil {
		t.Fatalf("他人不应可操作上传会话")
	}
	if err := svc.checkOwner(sess, admin); err != nil {
		t.Fatalf("管理员应可操作: %v", err)
	}
	if err := svc.checkOwner(sess, nil); err == nil {
		t.Fatalf("未登录不应可操作")
	}
}

func TestInitRejectsBadInputBeforeDiskWrites(t *testing.T) {
	// Init 在写入任何磁盘内容前就校验策略；这里用 nil store 验证「校验先于存储调用」。
	repo := &fakeSettingsRepo{}
	set, err := settings.New(t.Context(), repo)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	svc := &Service{set: set} // store / storage 故意为 nil：一旦走到落盘就会 panic

	owner := &model.User{ID: 1, Role: model.RoleUser, DirRel: "users/1"}

	// 先关闭上传开关：此时应直接拒绝，不会触达 store / storage。
	disabled := false
	if _, err := set.Update(t.Context(), settings.Patch{UploadEnabled: &disabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := svc.Init(t.Context(), InitInput{
		Owner: owner, FileName: "a.txt", SizeBytes: 1,
	}); err == nil {
		t.Fatalf("上传关闭时应拒绝")
	}

	// 打开上传，但限制类型为 pdf
	enabled := true
	exts := "pdf"
	if _, err := set.Update(t.Context(), settings.Patch{UploadEnabled: &enabled, AllowedExtensions: &exts}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 体积超限（应在落盘前拒绝）
	tooBig := int64(set.Get().MaxFileSizeBytes()) + 1
	if _, err := svc.Init(t.Context(), InitInput{
		Owner: owner, FileName: "ok.pdf", SizeBytes: tooBig,
	}); err == nil {
		t.Fatalf("超限体积应在 init 阶段被拒绝")
	}

	// 类型不允许（应在落盘前拒绝）
	if _, err := svc.Init(t.Context(), InitInput{
		Owner: owner, FileName: "run.exe", SizeBytes: 10,
	}); err == nil {
		t.Fatalf("不允许的类型应在 init 阶段被拒绝")
	}

	// 负数大小
	if _, err := svc.Init(t.Context(), InitInput{
		Owner: owner, FileName: "a.pdf", SizeBytes: -1,
	}); err == nil {
		t.Fatalf("负数大小应被拒绝")
	}

	// 未登录
	if _, err := svc.Init(t.Context(), InitInput{
		Owner: nil, FileName: "a.pdf", SizeBytes: 10,
	}); err == nil {
		t.Fatalf("未登录应被拒绝")
	}
}

// fakeSettingsRepo 是 settings.Repo 的最小内存实现。
type fakeSettingsRepo struct{ m map[string]string }

func (r *fakeSettingsRepo) AllSettings(_ context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.m {
		out[k] = v
	}
	return out, nil
}

func (r *fakeSettingsRepo) PutSettings(_ context.Context, kv map[string]string) error {
	if r.m == nil {
		r.m = map[string]string{}
	}
	for k, v := range kv {
		r.m[k] = v
	}
	return nil
}
