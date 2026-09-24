package shares

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"lan-drive/internal/model"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
)

// 分享的核心语义都能在 store 层验证（不需要 HTTP）。
// 时间相关字段用 DATETIME（秒级），比较时统一 Truncate。

func newSvc(t *testing.T) (*Service, *store.Store, *storage.Storage) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("LANDRIVE_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skipf("未设置 LANDRIVE_TEST_MYSQL_DSN，跳过分享集成测试")
	}
	dsn = withDBTag(dsn, "shares")
	ctx := context.Background()
	autoCreate := strings.ToLower(strings.TrimSpace(os.Getenv("LANDRIVE_TEST_AUTO_CREATE_DB"))) != "false"
	st, err := store.Open(ctx, dsn, autoCreate)
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, table := range []string{"shares", "files", "folders", "user_pins", "upload_chunks", "upload_sessions", "users", "settings"} {
		if _, err := st.DB().ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("清理表 %s 失败: %v", table, err)
		}
	}
	disk, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	set, err := settings.New(ctx, st)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	return New(st, disk, set), st, disk
}

func mkUser(t *testing.T, st *store.Store, no, name string) *model.User {
	t.Helper()
	ctx := context.Background()
	u := &model.User{
		EmployeeNo: no, Name: name, Password: "h", Role: model.RoleUser,
		Enabled: true, DirRel: "users/" + no,
	}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.SetUserDirRel(ctx, u.ID, u.DirRel); err != nil {
		t.Fatalf("SetUserDirRel: %v", err)
	}
	return u
}

// mkFile 建一条真实落盘的文件记录（内容写入磁盘，便于下载路径验证）。
func mkFile(t *testing.T, st *store.Store, disk *storage.Storage, owner *model.User, name, ext string) *model.File {
	t.Helper()
	ctx := context.Background()
	f := &model.File{
		OwnerID: owner.ID, OriginalNam: name, Ext: ext, SizeBytes: 5,
		Mime: storage.MIMEFor(ext, name), SHA256: strings.Repeat("a", 64),
		RelPath: owner.DirRel + "/tmp-" + name, Status: model.StatusActive,
		ExpiresAt: time.Now().UTC().AddDate(0, 0, 15),
	}
	if err := st.CreateFile(ctx, f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	// 用最终路径重写一次，模拟上传完成的落盘结果。
	finalRel := storage.FileRel(owner.DirRel, f.ID, ext)
	if _, err := disk.WriteChunk(finalRel, strings.NewReader("hello"), 100); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := st.FinishFile(ctx, f.ID, finalRel, strings.Repeat("a", 64), 5); err != nil {
		t.Fatalf("FinishFile: %v", err)
	}
	got, err := st.GetFileForShare(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFileForShare: %v", err)
	}
	return got
}

func intp(v int) *int { return &v }

// TestShareSurvivesRename 验证"文件修改后仍可访问"。
//
// 这是需求的核心：链接指向的是**记录**而不是磁盘路径，
// 因此改名（只改 original_name）不会让链接失效。
func TestShareSurvivesRename(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	owner := mkUser(t, st, "80001", "张三")
	f := mkFile(t, st, disk, owner, "报表.xlsx", ".xlsx")

	sh, err := svc.Create(ctx, CreateInput{Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 改名
	if err := st.RenameFile(ctx, f.ID, "季度报表.xlsx"); err != nil {
		t.Fatalf("RenameFile: %v", err)
	}

	res, err := svc.Resolve(ctx, sh.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != ResolveOK {
		t.Fatalf("改名后分享应仍可用，实际 %s", res.Status)
	}
	if res.Name != "季度报表.xlsx" {
		t.Fatalf("应展示改名后的名字，实际 %q", res.Name)
	}

	// 下载仍能拿到真实路径
	if _, abs, err := svc.FileForDownload(ctx, sh.Token); err != nil {
		t.Fatalf("FileForDownload: %v", err)
	} else if _, err := os.Stat(abs); err != nil {
		t.Fatalf("下载路径不存在: %v", err)
	}
}

// TestShareReportsDeletedTarget 验证"删除后提示分享的文件也被删除"，
// 以及管理员恢复后链接自动恢复可用。
func TestShareReportsDeletedTarget(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	owner := mkUser(t, st, "80002", "李四")
	f := mkFile(t, st, disk, owner, "资料.pdf", ".pdf")

	sh, err := svc.Create(ctx, CreateInput{Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res, _ := svc.Resolve(ctx, sh.Token); res.Status != ResolveOK {
		t.Fatalf("初始应可用，实际 %s", res.Status)
	}

	// 软删除（进回收站）
	now := time.Now().UTC()
	if err := st.MarkTrashed(ctx, f.ID, now, now.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("MarkTrashed: %v", err)
	}

	res, err := svc.Resolve(ctx, sh.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 关键：必须是 "deleted" 而不是 "notfound"，
	// 否则用户无法区分"文件被删了"与"链接是错的"。
	if res.Status != ResolveDeleted {
		t.Fatalf("软删后应返回 deleted，实际 %s", res.Status)
	}
	if res.Name != "资料.pdf" {
		t.Fatalf("应仍能告知被删的是哪个文件，实际 %q", res.Name)
	}
	// 此时不应还能下载
	if _, _, err := svc.FileForDownload(ctx, sh.Token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("已删除的文件不应可下载，err=%v", err)
	}

	// 管理员恢复 → 同一条链接应自动恢复可用
	if err := st.RestoreFile(ctx, f.ID, time.Now().UTC().AddDate(0, 0, 15)); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	res, err = svc.Resolve(ctx, sh.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != ResolveOK {
		t.Fatalf("恢复后分享应重新可用，实际 %s", res.Status)
	}
}

// TestShareGoneAfterPurge 验证记录被彻底清除后，分享靠外键级联消失。
func TestShareGoneAfterPurge(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	owner := mkUser(t, st, "80003", "王五")
	f := mkFile(t, st, disk, owner, "临时.txt", ".txt")

	sh, err := svc.Create(ctx, CreateInput{Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.DeleteFileRow(ctx, f.ID); err != nil {
		t.Fatalf("DeleteFileRow: %v", err)
	}
	// 分享行应已被 CASCADE 删除
	if _, err := st.GetShareByID(ctx, sh.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("文件记录删除后分享应一并消失，err=%v", err)
	}
	res, err := svc.Resolve(ctx, sh.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != ResolveNotFound {
		t.Fatalf("彻底清除后应返回 notfound，实际 %s", res.Status)
	}
}

// TestShareExpiry 验证有效期：永久、以及 1/3/7/30 天，且非法值被拒。
func TestShareExpiry(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	owner := mkUser(t, st, "80004", "赵六")
	f := mkFile(t, st, disk, owner, "有效期.txt", ".txt")

	// 永久（默认）
	perm, err := svc.Create(ctx, CreateInput{Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID})
	if err != nil {
		t.Fatalf("Create(永久): %v", err)
	}
	if perm.ExpireDays != nil || perm.ExpiresAt != nil {
		t.Fatalf("默认应为永久分享，实际 expire_days=%v expires_at=%v", perm.ExpireDays, perm.ExpiresAt)
	}

	// 四种合法有效期
	for _, days := range model.ShareExpireOptions {
		sh, err := svc.Create(ctx, CreateInput{
			Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID, ExpireDays: intp(days),
		})
		if err != nil {
			t.Fatalf("Create(%d 天): %v", days, err)
		}
		if sh.ExpireDays == nil || *sh.ExpireDays != days {
			t.Fatalf("expire_days 应为 %d，实际 %v", days, sh.ExpireDays)
		}
		want := time.Now().UTC().AddDate(0, 0, days)
		if sh.ExpiresAt == nil || sh.ExpiresAt.Sub(want).Abs() > 2*time.Minute {
			t.Fatalf("%d 天的到期时间不符：%v（期望约 %v）", days, sh.ExpiresAt, want)
		}
	}

	// 非法值必须被拒（防止前端传 0 或负数绕过）
	for _, bad := range []int{0, -1, 2, 365} {
		if _, err := svc.Create(ctx, CreateInput{
			Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID, ExpireDays: intp(bad),
		}); !errors.Is(err, ErrBadExpire) {
			t.Fatalf("有效期 %d 应被拒绝，err=%v", bad, err)
		}
	}

	// 已过期的分享应返回 expired
	sh, err := svc.Create(ctx, CreateInput{
		Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID, ExpireDays: intp(1),
	})
	if err != nil {
		t.Fatalf("Create(1 天): %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := st.DB().ExecContext(ctx, `UPDATE shares SET expires_at = ? WHERE id = ?`, past, sh.ID); err != nil {
		t.Fatalf("改到期时间: %v", err)
	}
	res, err := svc.Resolve(ctx, sh.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != ResolveExpired {
		t.Fatalf("过期后应返回 expired，实际 %s", res.Status)
	}
}

// TestSharePermission 验证"只能分享自己的"与"管理员可管理全部"。
func TestSharePermission(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	alice := mkUser(t, st, "80005", "阿丽")
	bob := mkUser(t, st, "80006", "阿波")
	// 管理员必须是真实行：shares.owner_id 有外键指向 users，
	// 用伪造 ID 会撞 fk_shares_owner（这正说明外键在起作用）。
	admin := mkUser(t, st, "80010", "管理员")
	admin.Role = model.RoleAdmin

	f := mkFile(t, st, disk, alice, "阿丽的文件.txt", ".txt")

	// 别人不能分享
	if _, err := svc.Create(ctx, CreateInput{Actor: bob, TargetType: model.ShareTargetFile, TargetID: f.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非属主分享应被拒，err=%v", err)
	}
	// 管理员可以
	sh, err := svc.Create(ctx, CreateInput{Actor: admin, TargetType: model.ShareTargetFile, TargetID: f.ID})
	if err != nil {
		t.Fatalf("管理员应可分享任意文件: %v", err)
	}

	// 别人不能撤销；管理员可以
	if err := svc.Revoke(ctx, sh.ID, bob); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非创建者撤销应被拒，err=%v", err)
	}
	if err := svc.Revoke(ctx, sh.ID, admin); err != nil {
		t.Fatalf("管理员应可撤销: %v", err)
	}
	if _, err := svc.Resolve(ctx, sh.Token); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 撤销后应失效
	res, _ := svc.Resolve(ctx, sh.Token)
	if res.Status != ResolveNotFound {
		t.Fatalf("撤销后应 notfound，实际 %s", res.Status)
	}
}

// TestShareListVisibleToEveryone 验证"所有人都能看到所有人创建的分享"。
func TestShareListVisibleToEveryone(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	alice := mkUser(t, st, "80007", "甲")
	bob := mkUser(t, st, "80008", "乙")

	fa := mkFile(t, st, disk, alice, "甲的文件.txt", ".txt")
	fb := mkFile(t, st, disk, bob, "乙的文件.txt", ".txt")
	if _, err := svc.Create(ctx, CreateInput{Actor: alice, TargetType: model.ShareTargetFile, TargetID: fa.ID}); err != nil {
		t.Fatalf("Create(alice): %v", err)
	}
	if _, err := svc.Create(ctx, CreateInput{Actor: bob, TargetType: model.ShareTargetFile, TargetID: fb.ID}); err != nil {
		t.Fatalf("Create(bob): %v", err)
	}

	// 甲应能看到乙创建的分享
	items, total, err := svc.List(ctx, alice, false, 1, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 {
		t.Fatalf("所有人都应看到全部 2 条分享，实际 %d", total)
	}
	owners := map[string]bool{}
	names := map[string]bool{}
	for _, it := range items {
		owners[it.OwnerName] = true
		names[it.TargetName] = true
	}
	if !owners["甲"] || !owners["乙"] {
		t.Fatalf("列表应带创建者姓名，实际 %v", owners)
	}
	if !names["甲的文件.txt"] || !names["乙的文件.txt"] {
		t.Fatalf("列表应带目标名，实际 %v", names)
	}

	// onlyMine 只看自己
	_, mine, err := svc.List(ctx, alice, true, 1, 50)
	if err != nil {
		t.Fatalf("List(onlyMine): %v", err)
	}
	if mine != 1 {
		t.Fatalf("只看自己的应只有 1 条，实际 %d", mine)
	}
}

// TestShareDeletedTargetListed 验证列表里能看出目标已被删除（前端要提示）。
func TestShareDeletedTargetListed(t *testing.T) {
	svc, st, disk := newSvc(t)
	ctx := context.Background()
	owner := mkUser(t, st, "80009", "丙")
	f := mkFile(t, st, disk, owner, "会被删.txt", ".txt")
	if _, err := svc.Create(ctx, CreateInput{Actor: owner, TargetType: model.ShareTargetFile, TargetID: f.ID}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now().UTC()
	if err := st.MarkTrashed(ctx, f.ID, now, now.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("MarkTrashed: %v", err)
	}
	items, _, err := svc.List(ctx, owner, false, 1, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || !items[0].TargetDeleted {
		t.Fatalf("列表应标出目标已删除，实际 %+v", items)
	}
}

// withDBTag 与 store 包测试保持一致：给库名加后缀避免并行测试互相清库。
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
