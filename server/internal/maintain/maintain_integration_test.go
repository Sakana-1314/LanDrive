// 维护任务与完整上传链路的集成测试：需要真实 MySQL（与 storage 用临时目录）。
//
// 未设置 LANDRIVE_TEST_MYSQL_DSN 时自动跳过。
//
//	LANDRIVE_TEST_MYSQL_DSN='root:@tcp(127.0.0.1:3306)/lanfs_test' go test ./internal/maintain/ -v
package maintain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lan-drive/internal/files"
	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
	"lan-drive/internal/upload"
)

const envDSN = "LANDRIVE_TEST_MYSQL_DSN"

type fixture struct {
	st    *store.Store
	disk  *storage.Storage
	set   *settings.Service
	svc   *Service
	up    *upload.Service
	admin *model.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(envDSN))
	if dsn == "" {
		t.Skipf("未设置 %s，跳过集成测试", envDSN)
	}
	dsn = withDBTag(dsn, "maintain")
	ctx := context.Background()

	// 与 store 包测试保持一致：默认自动建库；置 LANDRIVE_TEST_AUTO_CREATE_DB=false
	// 可改用已存在的库。部分 MySQL 兼容实现经 CREATE DATABASE 建库时外键元数据
	// 不完整（会报 missing index for foreign key），这类环境必须走这个开关。
	autoCreate := strings.ToLower(strings.TrimSpace(os.Getenv("LANDRIVE_TEST_AUTO_CREATE_DB"))) != "false"
	st, err := store.Open(ctx, dsn, autoCreate)
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	for _, table := range []string{"user_pins", "upload_chunks", "upload_sessions", "files", "users", "settings"} {
		if _, err := st.DB().ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("清理表 %s 失败: %v", table, err)
		}
	}

	disk, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	if err := settings.Seed(ctx, st); err != nil {
		t.Fatalf("播种配置失败: %v", err)
	}
	set, err := settings.New(ctx, st)
	if err != nil {
		t.Fatalf("载入配置失败: %v", err)
	}

	admin := &model.User{
		EmployeeNo: "admin", Name: "管理员", Password: "hash",
		Role: model.RoleAdmin, Enabled: true, DirRel: "users/0",
	}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	dirRel, _ := disk.EnsureUserDir(admin.EmployeeNo)
	if err := st.SetUserDirRel(ctx, admin.ID, dirRel); err != nil {
		t.Fatalf("设置目录失败: %v", err)
	}
	admin.DirRel = dirRel

	return &fixture{
		st: st, disk: disk, set: set,
		svc:   New(st, disk, set),
		up:    upload.New(st, disk, set, nil),
		admin: admin,
	}
}

// uploadBytes 走完整的分片上传流程（init → 分片 → complete），返回最终文件。
func (f *fixture) uploadBytes(t *testing.T, name string, data []byte) *model.File {
	t.Helper()
	ctx := context.Background()

	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: name, SizeBytes: int64(len(data)),
	})
	if err != nil {
		t.Fatalf("Init(%s): %v", name, err)
	}
	chunkSize := int64(sess.ChunkSize)
	for i := 0; i < sess.TotalChunks; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		var body []byte
		if start < int64(len(data)) {
			body = data[start:end]
		}
		if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, i, bytes.NewReader(body)); err != nil {
			t.Fatalf("WriteChunk(%s, %d): %v", name, i, err)
		}
	}
	res, err := f.up.Complete(ctx, sess.ID, f.admin)
	if err != nil {
		t.Fatalf("Complete(%s): %v", name, err)
	}
	return res.File
}

// TestFullUploadPipeline 验证「磁盘落盘 + 数据库记录 + 相对路径」三者完全一致。
func TestFullUploadPipeline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	data := bytes.Repeat([]byte("lan-drive-integration-"), 5000) // ~105 KB
	file := f.uploadBytes(t, "集成测试报告.txt", data)

	// 数据库记录
	if file.SizeBytes != int64(len(data)) {
		t.Fatalf("记录大小 = %d，期望 %d", file.SizeBytes, len(data))
	}
	if file.SHA256 != storage.HashBytes(data) {
		t.Fatalf("sha256 不一致")
	}
	if file.Status != model.StatusActive {
		t.Fatalf("上传后状态应为 active，实际 %q", file.Status)
	}

	// rel_path 必须落在属主目录下，且与主键 + 扩展名一致
	wantRel := storage.FileRel(f.admin.DirRel, file.ID, ".txt")
	if file.RelPath != wantRel {
		t.Fatalf("rel_path = %q，期望 %q", file.RelPath, wantRel)
	}

	// 磁盘上必须真实存在，且内容与上传一致
	abs, err := f.disk.Abs(file.RelPath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("磁盘文件不存在: %v", err)
	}
	if !bytes.Equal(onDisk, data) {
		t.Fatalf("磁盘内容与上传内容不一致")
	}

	// 分片目录必须已清理（合并成功后临时分片不再保留）
	entries, err := os.ReadDir(filepath.Join(f.disk.Root(), "tmp", "chunks"))
	if err != nil {
		t.Fatalf("读取分片目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("合并后分片目录应被清理，仍有 %d 项", len(entries))
	}

	// 到期时间应约为 15 天后
	days := int(time.Until(file.ExpiresAt).Hours() / 24)
	if days < 14 || days > 15 {
		t.Fatalf("到期时间距今 %d 天，期望约 15 天", days)
	}

	// 一致性扫描应报告完全一致
	rep, err := f.svc.ScanStorage(ctx)
	if err != nil {
		t.Fatalf("ScanStorage: %v", err)
	}
	if len(rep.Orphans) != 0 || len(rep.Missing) != 0 || len(rep.Invalid) != 0 {
		t.Fatalf("一致性扫描异常: 孤儿 %v 缺失 %v 非法 %v", rep.Orphans, rep.Missing, rep.Invalid)
	}
}

// TestUploadRejectsWrongSHA 验证客户端声明的 sha256 不符时拒绝合并。
func TestUploadRejectsWrongSHA(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	data := []byte("hello world")

	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: "错哈希.txt", SizeBytes: int64(len(data)),
		SHA256: strings.Repeat("f", 64), // 故意给错的
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 0, bytes.NewReader(data)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if _, err := f.up.Complete(ctx, sess.ID, f.admin); err == nil {
		t.Fatalf("sha256 不符时应拒绝合并")
	}
	// 失败后不应留下任何文件记录
	if _, total, _ := f.st.ListFiles(ctx, store.FileQuery{Status: "all", Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("合并失败后不应残留文件记录，实际 %d 条", total)
	}
	// 也不应残留磁盘文件
	if err := f.disk.WalkFiles("users", func(string, os.FileInfo) error {
		t.Fatalf("合并失败后不应有磁盘文件残留")
		return nil
	}); err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
}

// TestIncompleteChunksRejected 验证分片不齐时不得合并。
func TestIncompleteChunksRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// 声明 3 个分片，只传 1 个
	size := int64(f.set.Get().ChunkSizeBytes())*2 + 100
	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: "不完整.bin", SizeBytes: size,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if sess.TotalChunks != 3 {
		t.Fatalf("总分片数 = %d，期望 3", sess.TotalChunks)
	}
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 0, bytes.NewReader(make([]byte, sess.ChunkSize))); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if _, err := f.up.Complete(ctx, sess.ID, f.admin); err == nil {
		t.Fatalf("分片不齐时应拒绝合并")
	}
	// 越界分片应被拒绝
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 99, bytes.NewReader(nil)); err == nil {
		t.Fatalf("越界分片序号应被拒绝")
	}
	// 单分片超长应被拒绝
	tooBig := make([]byte, sess.ChunkSize+10)
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 1, bytes.NewReader(tooBig)); err == nil {
		t.Fatalf("超长分片应被拒绝")
	}
}

// TestExpireThenPurgeLifecycle 是核心需求的端到端验证：
// 上传 → 15 天后标记删除（对普通用户不可见）→ 再 7 天后彻底删除磁盘文件与记录。
func TestExpireThenPurgeLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	data := []byte("会被自动清理的文件内容")
	file := f.uploadBytes(t, "临时文件.txt", data)

	abs, err := f.disk.Abs(file.RelPath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("上传后磁盘文件应存在: %v", err)
	}

	// 1) 把到期时间改到过去，模拟「上传满 15 天」。
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := f.st.DB().ExecContext(ctx,
		`UPDATE files SET expires_at = ? WHERE id = ?`, past, file.ID); err != nil {
		t.Fatalf("准备到期数据失败: %v", err)
	}

	// 执行到期标记
	if n := f.svc.RunExpire(ctx); n != 1 {
		t.Fatalf("到期标记应处理 1 个文件，实际 %d", n)
	}

	got, err := f.st.GetFile(ctx, file.ID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got.Status != model.StatusTrashed {
		t.Fatalf("到期后状态 = %q，期望 trashed", got.Status)
	}
	if got.DeletedAt == nil || got.PurgeAt == nil {
		t.Fatalf("到期后应记录删除时间与彻底删除时间")
	}
	// 回收站保留期应约为 7 天
	keep := int(got.PurgeAt.Sub(*got.DeletedAt).Hours() / 24)
	if keep != 7 {
		t.Fatalf("回收站保留 %d 天，期望 7 天", keep)
	}

	// 普通用户列表中不可见
	_, total, err := f.st.ListFiles(ctx, store.FileQuery{Status: model.StatusActive, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if total != 0 {
		t.Fatalf("到期文件不应出现在 active 列表中")
	}

	// 此时物理清理不应删除（还没到 purge_at）
	if n := f.svc.RunPurge(ctx); n != 0 {
		t.Fatalf("尚未到彻底删除时间，不应删除任何文件，实际 %d", n)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("回收站保留期内磁盘文件应仍存在（可恢复）: %v", err)
	}

	// 2) 把 purge_at 改到过去，模拟「回收站也满 7 天」。
	pastPurge := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := f.st.DB().ExecContext(ctx,
		`UPDATE files SET purge_at = ? WHERE id = ?`, pastPurge, file.ID); err != nil {
		t.Fatalf("准备清理数据失败: %v", err)
	}
	if n := f.svc.RunPurge(ctx); n != 1 {
		t.Fatalf("物理清理应删除 1 个文件，实际 %d", n)
	}

	// 磁盘文件必须已删除
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("彻底删除后磁盘文件应不存在")
	}
	// 数据库记录必须已删除
	if _, err := f.st.GetFile(ctx, file.ID); err != store.ErrNotFound {
		t.Fatalf("彻底删除后记录应不存在，实际 %v", err)
	}
	// 空用户目录应被清理
	if entries, err := os.ReadDir(filepath.Join(f.disk.Root(), "users", fmt.Sprint(f.admin.ID))); err == nil && len(entries) == 0 {
		t.Fatalf("空用户目录应被清理")
	}
}

// TestExpireRespectsConfiguredDays 验证保留天数与回收站天数均可配置。
func TestExpireRespectsConfiguredDays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	retention := 3
	trash := 2
	if _, err := f.set.Update(ctx, settings.Patch{RetentionDays: &retention, TrashDays: &trash}); err != nil {
		t.Fatalf("更新配置: %v", err)
	}

	file := f.uploadBytes(t, "三天后到期.txt", []byte("x"))

	// 新上传的文件应按新配置计算到期时间
	days := int(time.Until(file.ExpiresAt).Hours() / 24)
	if days < 2 || days > 3 {
		t.Fatalf("到期时间距今 %d 天，期望约 3 天", days)
	}

	// 到期 → 回收站，保留期应为 2 天
	earlier := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE files SET expires_at = ? WHERE id = ?`, earlier, file.ID); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if n := f.svc.RunExpire(ctx); n != 1 {
		t.Fatalf("应标记 1 个文件，实际 %d", n)
	}
	got, _ := f.st.GetFile(ctx, file.ID)
	keep := int(got.PurgeAt.Sub(*got.DeletedAt).Hours() / 24)
	if keep != 2 {
		t.Fatalf("回收站保留 %d 天，期望 2 天（配置未生效）", keep)
	}
}

// TestRestoreThenExpireAgain 验证管理员恢复后重新计时。
func TestRestoreThenExpireAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	file := f.uploadBytes(t, "可恢复.txt", []byte("restore me"))

	// 到期标记
	earlier := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := f.st.DB().ExecContext(ctx, `UPDATE files SET expires_at = ? WHERE id = ?`, earlier, file.ID); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	if n := f.svc.RunExpire(ctx); n != 1 {
		t.Fatalf("应标记 1 个文件，实际 %d", n)
	}

	// 恢复（模拟管理员操作：到期时间 = 现在 + retention_days）
	newExpiry := time.Now().UTC().AddDate(0, 0, f.set.Get().RetentionDays)
	if err := f.st.RestoreFile(ctx, file.ID, newExpiry); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	got, _ := f.st.GetFile(ctx, file.ID)
	if got.Status != model.StatusActive || got.DeletedAt != nil || got.PurgeAt != nil {
		t.Fatalf("恢复后状态不正确: %+v", got)
	}
	// 恢复后不应再被到期任务处理
	if n := f.svc.RunExpire(ctx); n != 0 {
		t.Fatalf("恢复后的文件不应立即再次到期，实际处理 %d", n)
	}
}

// TestCleanStaleUploads 验证僵尸上传会话与残留分片会被清理。
func TestCleanStaleUploads(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: "被放弃的上传.bin", SizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 0, bytes.NewReader([]byte("0123456789"))); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	// 分片目录应已存在
	chunkDir, _ := f.disk.Abs(storage.ChunkDirRel(sess.ID))
	if _, err := os.Stat(chunkDir); err != nil {
		t.Fatalf("分片目录应存在: %v", err)
	}

	// 把会话的 updated_at 改到 25 小时前（超过 24 小时阈值）
	old := time.Now().UTC().Add(-25 * time.Hour)
	if _, err := f.st.DB().ExecContext(ctx,
		`UPDATE upload_sessions SET updated_at = ? WHERE id = ?`, old, sess.ID); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	if n := f.svc.RunCleanStaleUploads(ctx); n < 1 {
		t.Fatalf("应清理至少 1 个僵尸会话，实际 %d", n)
	}
	// 会话记录与分片目录都应被清理
	if _, err := f.st.GetUploadSession(ctx, sess.ID); err != store.ErrNotFound {
		t.Fatalf("僵尸会话应被删除，实际 %v", err)
	}
	if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
		t.Fatalf("僵尸会话的分片目录应被删除")
	}
}

// TestOrphanScanArchivesUnownedFiles 验证磁盘上无主文件会被归档而非误删。
func TestOrphanScanArchivesUnownedFiles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// 造一个数据库里没有记录的「孤儿文件」
	orphanRel := storage.FileRel(f.admin.DirRel, 99999, ".txt")
	if _, err := f.disk.WriteChunk(orphanRel, strings.NewReader("孤儿"), 100); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	// 先做只读扫描：应报告为孤儿，但不动文件
	rep, err := f.svc.ScanStorage(ctx)
	if err != nil {
		t.Fatalf("ScanStorage: %v", err)
	}
	if len(rep.Orphans) != 1 || rep.Orphans[0] != orphanRel {
		t.Fatalf("应报告 1 个孤儿文件，实际 %v", rep.Orphans)
	}
	if !f.disk.Exists(orphanRel) {
		t.Fatalf("只读扫描不应移动文件")
	}

	// 每日任务：归档孤儿
	if n := f.svc.ScanOrphans(ctx); n != 1 {
		t.Fatalf("应归档 1 个孤儿文件，实际 %d", n)
	}
	if f.disk.Exists(orphanRel) {
		t.Fatalf("归档后原位置不应再有文件")
	}
	// 归档到 tmp/orphans/<日期>/... 下（保留 7 天，可人工核对）
	found := false
	if err := f.disk.WalkFiles("tmp/orphans", func(rel string, _ os.FileInfo) error {
		found = true
		return nil
	}); err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	if !found {
		t.Fatalf("孤儿文件应被归档到 tmp/orphans 下")
	}
}

// TestUploadedChunksNotReportedAsOrphans 是针对一致性扫描误报的回归测试：
// 正在进行的上传分片位于 tmp/ 下且由 upload_chunks 管理，不能算孤儿。
func TestUploadedChunksNotReportedAsOrphans(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: "上传中.bin", SizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 0, bytes.NewReader([]byte("0123456789"))); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	rep, err := f.svc.ScanStorage(ctx)
	if err != nil {
		t.Fatalf("ScanStorage: %v", err)
	}
	if len(rep.Orphans) != 0 {
		t.Fatalf("正在上传的分片不应被报为孤儿，实际 %v", rep.Orphans)
	}
}

func TestMaintenanceLockIsExclusive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	ran := 0
	f.svc.withLock(ctx, "test", func(context.Context) { ran++ })
	if ran != 1 {
		t.Fatalf("首次执行应获得锁，实际 ran=%d", ran)
	}

	// 外部先持有同名锁，withLock 应跳过任务体。
	ok, release, err := f.st.TryLock(ctx, lockName, 0)
	if err != nil || !ok {
		t.Fatalf("外部持锁失败: ok=%v err=%v", ok, err)
	}
	f.svc.withLock(ctx, "test", func(context.Context) { ran++ })
	if ran != 1 {
		t.Fatalf("锁被占用时不应执行任务体，实际 ran=%d", ran)
	}
	release()

	// 释放后又能执行
	f.svc.withLock(ctx, "test", func(context.Context) { ran++ })
	if ran != 2 {
		t.Fatalf("释放后应能执行，实际 ran=%d", ran)
	}
}

// withDBTag 与 store 包测试保持一致：给库名加后缀，避免并行测试互相清库。
func withDBTag(dsn, tag string) string {
	base, params := dsn, ""
	if i := strings.Index(dsn, "?"); i >= 0 {
		base, params = dsn[:i], dsn[i:]
	}
	slash := strings.LastIndex(base, "/")
	if slash < 0 {
		return dsn
	}
	dbName := base[slash+1:]
	if dbName == "" {
		return dsn
	}
	return base[:slash+1] + dbName + "_" + tag + params
}

// TestCompleteReturnsDisplayFields 是针对一个真实缺陷的回归测试：
// 上传完成接口曾直接返回数据库原始行，导致前端拿到 days_left=0、can_edit=false。
// 所有对外返回文件的地方都必须经过 files.Decorate。
func TestCompleteReturnsDisplayFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// 复刻 main.go 的注入方式：upload 使用 files 的展示逻辑。
	filesSvc := files.New(f.st, f.disk, f.set)
	f.up = upload.New(f.st, f.disk, f.set, filesSvc.Decorate)

	file := f.uploadBytes(t, "展示字段.txt", []byte("hello"))

	if file.DaysLeft <= 0 {
		t.Fatalf("days_left = %d，新上传的文件应为正数（约 15 天）", file.DaysLeft)
	}
	if file.DaysLeft < 14 || file.DaysLeft > 15 {
		t.Fatalf("days_left = %d，期望约 15 天", file.DaysLeft)
	}
	if !file.IsMine {
		t.Fatalf("属主上传的文件 is_mine 应为 true")
	}
	if !file.CanEdit {
		t.Fatalf("属主刚上传的文件 can_edit 应为 true")
	}

	// 与列表接口返回的字段保持一致。
	got, err := filesSvc.Get(ctx, file.ID, f.admin)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DaysLeft != file.DaysLeft || got.IsMine != file.IsMine || got.CanEdit != file.CanEdit {
		t.Fatalf("上传接口与查询接口的展示字段不一致: %+v vs %+v", file, got)
	}

	// 重复 complete（幂等分支）同样必须带展示字段。
	sess, err := f.up.Init(ctx, upload.InitInput{
		Owner: f.admin, FileName: "幂等展示.txt", SizeBytes: 3,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := f.up.WriteChunk(ctx, sess, f.admin, 0, bytes.NewReader([]byte("abc"))); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	first, err := f.up.Complete(ctx, sess.ID, f.admin)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if first.File.DaysLeft <= 0 || !first.File.CanEdit {
		t.Fatalf("首次 complete 的展示字段缺失: %+v", first.File)
	}
	second, err := f.up.Complete(ctx, sess.ID, f.admin)
	if err != nil {
		t.Fatalf("重复 Complete: %v", err)
	}
	if second.Created {
		t.Fatalf("重复 complete 应返回 created=false")
	}
	if second.File.ID != first.File.ID {
		t.Fatalf("重复 complete 应返回同一文件")
	}
	if second.File.DaysLeft <= 0 || !second.File.CanEdit {
		t.Fatalf("幂等分支的展示字段缺失: %+v", second.File)
	}
}
