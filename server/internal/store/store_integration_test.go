// 数据库集成测试：需要真实 MySQL。
//
// 未设置 LANDRIVE_TEST_MYSQL_DSN 时自动跳过（因此 `make test` 在没有数据库的
// 机器上也能全绿）。设置后会在独立库中完整走一遍 schema 迁移与仓储方法：
//
//	LANDRIVE_TEST_MYSQL_DSN='lanfs:pass@tcp(127.0.0.1:3306)/lanfs_test' go test ./internal/store/ -v
//
// 注意：测试会清空该库中的业务表数据，请务必使用独立的测试库。
//
// 库名会自动追加 _store / _maintain 后缀（见 withDBTag），因为 go test 默认
// 并行执行不同包，共用一个库会导致相互清库。
//
// 若使用 MySQL 兼容实现（如内嵌的测试引擎），建议预先建好库并设置
// LANDRIVE_TEST_AUTO_CREATE_DB=false：部分兼容实现经 CREATE DATABASE 建库时
// 外键元数据不完整，会让「删除被引用的父行应失败」这类断言误报。
package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"lan-drive/internal/model"
)

const envDSN = "LANDRIVE_TEST_MYSQL_DSN"

// pkgDBTag 用于给测试库名加后缀：go test 会并行执行不同包，
// 若共用同一个库，各包的「清空业务表」会互相破坏对方的数据。
const pkgDBTag = "store"

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(envDSN))
	if dsn == "" {
		t.Skipf("未设置 %s，跳过数据库集成测试", envDSN)
	}
	dsn = withDBTag(dsn, pkgDBTag)
	ctx := context.Background()
	// 默认自动建库；置 LANDRIVE_TEST_AUTO_CREATE_DB=false 可改用已存在的库
	// （部分 MySQL 兼容实现经 CREATE DATABASE 建库时外键元数据不完整）。
	autoCreate := strings.ToLower(strings.TrimSpace(os.Getenv("LANDRIVE_TEST_AUTO_CREATE_DB"))) != "false"
	st, err := Open(ctx, dsn, autoCreate)
	if err != nil {
		t.Fatalf("连接测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// 清空业务表，保证测试可重复运行。
	for _, table := range []string{"op_logs", "upload_chunks", "upload_sessions", "files", "users", "settings"} {
		if _, err := st.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("清理表 %s 失败: %v", table, err)
		}
	}
	return st
}

func TestMigrateCreatesAllTables(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	want := map[string]bool{
		"users": false, "files": false, "upload_sessions": false,
		"upload_chunks": false, "settings": false, "op_logs": false,
		"schema_migrations": false,
	}
	rows, err := st.db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		t.Fatalf("SHOW TABLES: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for table, found := range want {
		if !found {
			t.Fatalf("表 %s 未被创建", table)
		}
	}

	v, err := st.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v < 1 {
		t.Fatalf("迁移版本 = %d，期望 >= 1", v)
	}

	// 迁移必须幂等：再跑一次不应报错。
	if err := st.migrate(ctx); err != nil {
		t.Fatalf("重复迁移应幂等: %v", err)
	}
}

func TestUserRepository(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	u := &model.User{
		EmployeeNo: "10001", Name: "张三", Password: "hash-1",
		Role: model.RoleUser, Enabled: true, DirRel: "users/0",
	}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Fatalf("CreateUser 应回填自增 ID")
	}
	// 目录名依赖 ID，这里按真实流程回填。
	dirRel := "users/" + itoaTest(u.ID)
	if err := st.SetUserDirRel(ctx, u.ID, dirRel); err != nil {
		t.Fatalf("SetUserDirRel: %v", err)
	}

	// 工号唯一
	dup := &model.User{EmployeeNo: "10001", Name: "李四", Password: "h", Role: model.RoleUser, Enabled: true}
	if err := st.CreateUser(ctx, dup); err == nil {
		t.Fatalf("重复工号应报冲突")
	}

	// 按工号 / 按 ID 查询
	got, err := st.GetUserByEmployeeNo(ctx, "10001")
	if err != nil {
		t.Fatalf("GetUserByEmployeeNo: %v", err)
	}
	if got.Name != "张三" || got.DirRel != dirRel || !got.Enabled {
		t.Fatalf("查询结果不正确: %+v", got)
	}
	if _, err := st.GetUserByID(ctx, u.ID); err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if _, err := st.GetUserByEmployeeNo(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("不存在的工号应返回 ErrNotFound，实际 %v", err)
	}

	// 更新（姓名 / 角色 / 停用）
	name := "张三丰"
	role := model.RoleAdmin
	disabled := false
	if err := st.UpdateUser(ctx, u.ID, UserUpdate{Name: &name, Role: &role, Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, _ = st.GetUserByID(ctx, u.ID)
	if got.Name != "张三丰" || got.Role != model.RoleAdmin || got.Enabled {
		t.Fatalf("更新未生效: %+v", got)
	}

	// 统计：CountAdmins 只统计「启用状态」的管理员，此时用户已被停用，故为 0。
	if n, err := st.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d, err=%v", n, err)
	}
	if n, err := st.CountAdmins(ctx); err != nil || n != 0 {
		t.Fatalf("停用后的管理员不应计入 CountAdmins，实际 %d, err=%v", n, err)
	}
	// 重新启用后应计入
	enabled := true
	if err := st.UpdateUser(ctx, u.ID, UserUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("重新启用失败: %v", err)
	}
	if n, err := st.CountAdmins(ctx); err != nil || n != 1 {
		t.Fatalf("启用后的管理员应计入 CountAdmins，实际 %d, err=%v", n, err)
	}

	// 最后登录时间
	if err := st.TouchLastLogin(ctx, u.ID); err != nil {
		t.Fatalf("TouchLastLogin: %v", err)
	}
	got, _ = st.GetUserByID(ctx, u.ID)
	if got.LastLoginAt == nil {
		t.Fatalf("最后登录时间未写入")
	}

	// 删除
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.GetUserByID(ctx, u.ID); err != ErrNotFound {
		t.Fatalf("删除后应查不到")
	}
	if err := st.DeleteUser(ctx, 999999); err != ErrNotFound {
		t.Fatalf("删除不存在的用户应返回 ErrNotFound，实际 %v", err)
	}
}

func TestFileRepositoryLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	owner := createTestUser(t, st, "20001", "王五")
	now2 := now.AddDate(0, 0, 15)

	f := &model.File{
		OwnerID: owner.ID, OriginalNam: "报表.xlsx", Ext: ".xlsx",
		SizeBytes: 2048, Mime: "application/octet-stream", SHA256: strings.Repeat("a", 64),
		RelPath: "users/" + itoaTest(owner.ID) + "/1.xlsx",
		Status:  model.StatusActive, ExpiresAt: now2,
	}
	if err := st.CreateFile(ctx, f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if f.ID == 0 {
		t.Fatalf("CreateFile 应回填 ID")
	}

	// 占位路径 → 最终路径
	sha := strings.Repeat("b", 64)
	finalRel := "users/" + itoaTest(owner.ID) + "/" + itoaTest(f.ID) + ".xlsx"
	if err := st.FinishFile(ctx, f.ID, finalRel, sha, 4096); err != nil {
		t.Fatalf("FinishFile: %v", err)
	}
	got, err := st.GetFile(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got.RelPath != finalRel || got.SHA256 != sha || got.SizeBytes != 4096 {
		t.Fatalf("FinishFile 未生效: %+v", got)
	}
	// 联查应带上属主信息
	if got.OwnerName != "王五" || got.OwnerEmployeeNo != "20001" {
		t.Fatalf("属主联查信息缺失: %+v", got)
	}

	// rel_path 唯一
	dup := *f
	dup.ID = 0
	dup.RelPath = finalRel
	if err := st.CreateFile(ctx, &dup); err == nil {
		t.Fatalf("重复 rel_path 应报冲突")
	}

	// 重命名只改展示名
	if err := st.RenameFile(ctx, f.ID, "改名后.xlsx"); err != nil {
		t.Fatalf("RenameFile: %v", err)
	}
	got, _ = st.GetFile(ctx, f.ID)
	if got.OriginalNam != "改名后.xlsx" || got.RelPath != finalRel {
		t.Fatalf("重命名不应影响 rel_path: %+v", got)
	}

	// 列表 / 检索 / 分页
	list, total, err := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListFiles = (%d, %d), err=%v", len(list), total, err)
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Keyword: "王五", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("按属主姓名搜索应命中")
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Keyword: "改名后", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("按文件名搜索应命中")
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Keyword: "不存在", Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("不匹配的关键字应返回 0 条")
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Ext: ".xlsx", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("按扩展名过滤应命中")
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Ext: ".pdf", Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("不匹配的扩展名应返回 0 条")
	}
	// 排序白名单（含注入尝试）不应报错
	for _, s := range []string{"created_at", "size_bytes", "expires_at", "original_name", "owner", "'; DROP TABLE files; --"} {
		if _, _, err := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Sort: s, Order: "asc", Page: 1, PageSize: 10}); err != nil {
			t.Fatalf("排序键 %q 应安全处理: %v", s, err)
		}
	}

	// 软删除 → 回收站
	purgeAt := now.AddDate(0, 0, 7)
	if err := st.MarkTrashed(ctx, f.ID, now, purgeAt); err != nil {
		t.Fatalf("MarkTrashed: %v", err)
	}
	got, _ = st.GetFile(ctx, f.ID)
	if got.Status != model.StatusTrashed || got.DeletedAt == nil || got.PurgeAt == nil {
		t.Fatalf("软删除状态不正确: %+v", got)
	}
	// 只查 active 时不应出现
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusActive, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("回收站文件不应出现在 active 列表中")
	}
	if _, total, _ := st.ListFiles(ctx, FileQuery{Status: model.StatusTrashed, Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("回收站列表应包含该文件")
	}
	// 重复软删除应报状态错误
	if err := st.MarkTrashed(ctx, f.ID, now, purgeAt); err != ErrState {
		t.Fatalf("重复软删除应返回 ErrState，实际 %v", err)
	}

	// 恢复：到期时间重算，回收站字段清空
	newExpiry := now.AddDate(0, 0, 15)
	if err := st.RestoreFile(ctx, f.ID, newExpiry); err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	got, _ = st.GetFile(ctx, f.ID)
	if got.Status != model.StatusActive || got.DeletedAt != nil || got.PurgeAt != nil {
		t.Fatalf("恢复后状态不正确: %+v", got)
	}
	if !got.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("恢复后到期时间 = %v，期望 %v", got.ExpiresAt, newExpiry)
	}

	// 到期扫描
	past := now.Add(-time.Hour)
	if _, err := st.db.ExecContext(ctx, `UPDATE files SET expires_at = ? WHERE id = ?`, past, f.ID); err != nil {
		t.Fatalf("准备到期数据: %v", err)
	}
	expired, err := st.ExpireFiles(ctx, now, 7)
	if err != nil {
		t.Fatalf("ExpireFiles: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("应有 1 个文件到期，实际 %d", len(expired))
	}
	got, _ = st.GetFile(ctx, f.ID)
	if got.Status != model.StatusTrashed || got.PurgeAt == nil {
		t.Fatalf("到期后应进入回收站并设置清理时间: %+v", got)
	}
	// 再跑一次不应重复处理
	if again, _ := st.ExpireFiles(ctx, now, 7); len(again) != 0 {
		t.Fatalf("已处理的文件不应重复到期")
	}

	// 物理清理候选
	if list, err := st.ListPurgeable(ctx, now, 100); err != nil || len(list) != 0 {
		t.Fatalf("尚未到清理时间，应为空：%d, err=%v", len(list), err)
	}
	if list, err := st.ListPurgeable(ctx, now.AddDate(0, 0, 8), 100); err != nil || len(list) != 1 {
		t.Fatalf("到期后应成为清理候选：%d, err=%v", len(list), err)
	}

	// 路径集合与统计
	paths, err := st.AllRelPaths(ctx)
	if err != nil || len(paths) != 1 {
		t.Fatalf("AllRelPaths = %d, err=%v", len(paths), err)
	}

	// 彻底删除
	rel, err := st.DeleteFile(ctx, f.ID)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if rel != finalRel {
		t.Fatalf("DeleteFile 返回路径 = %q，期望 %q", rel, finalRel)
	}
	if _, err := st.GetFile(ctx, f.ID); err != ErrNotFound {
		t.Fatalf("彻底删除后应查不到")
	}
}

func TestSettingsRepository(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// 缺失的键
	if _, ok, err := st.GetSetting(ctx, "nope"); err != nil || ok {
		t.Fatalf("缺失键应返回 ok=false，err=%v", err)
	}

	kv := map[string]string{"a": "1", "b": "2"}
	if err := st.PutSettings(ctx, kv); err != nil {
		t.Fatalf("PutSettings: %v", err)
	}
	all, err := st.AllSettings(ctx)
	if err != nil || all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("AllSettings = %v, err=%v", all, err)
	}
	// 覆盖写（ON DUPLICATE KEY UPDATE）
	if err := st.PutSettings(ctx, map[string]string{"a": "9"}); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	if v, _, _ := st.GetSetting(ctx, "a"); v != "9" {
		t.Fatalf("覆盖写入未生效: %q", v)
	}
	// 空 map 不应报错
	if err := st.PutSettings(ctx, nil); err != nil {
		t.Fatalf("空写入不应报错: %v", err)
	}
}

func TestUploadRepositoryAndChunks(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner := createTestUser(t, st, "30001", "赵六")
	dirRel := "users/" + itoaTest(owner.ID)

	sess := &model.UploadSession{
		ID: strings.Repeat("f", 32), OwnerID: owner.ID, OriginalName: "大文件.bin",
		Ext: ".bin", SizeBytes: 10, ChunkSize: 4, TotalChunks: 3,
		Status: model.UploadUploading, DirRel: dirRel,
	}
	if err := st.CreateUploadSession(ctx, sess); err != nil {
		t.Fatalf("CreateUploadSession: %v", err)
	}
	if _, err := st.GetUploadSession(ctx, sess.ID); err != nil {
		t.Fatalf("GetUploadSession: %v", err)
	}

	// 断点续传的会话复用（同名同大小同 sha）
	found, err := st.FindOpenSession(ctx, owner.ID, "大文件.bin", 10, "")
	if err != nil {
		t.Fatalf("FindOpenSession: %v", err)
	}
	if found.ID != sess.ID {
		t.Fatalf("应复用同一会话")
	}
	// 不同大小不应复用
	if _, err := st.FindOpenSession(ctx, owner.ID, "大文件.bin", 999, ""); err != ErrNotFound {
		t.Fatalf("大小不同不应复用，err=%v", err)
	}

	// 分片写入是幂等的，且会同步累计字节
	if err := st.UpsertChunk(ctx, sess.ID, 0, 4, "tmp/chunks/x/0.part"); err != nil {
		t.Fatalf("UpsertChunk: %v", err)
	}
	if err := st.UpsertChunk(ctx, sess.ID, 1, 4, "tmp/chunks/x/1.part"); err != nil {
		t.Fatalf("UpsertChunk: %v", err)
	}
	got, _ := st.GetUploadSession(ctx, sess.ID)
	if got.ReceivedBytes != 8 {
		t.Fatalf("累计字节 = %d，期望 8", got.ReceivedBytes)
	}
	// 重复上传同一分片：大小不变，累计不应翻倍
	if err := st.UpsertChunk(ctx, sess.ID, 0, 4, "tmp/chunks/x/0.part"); err != nil {
		t.Fatalf("重复 UpsertChunk: %v", err)
	}
	got, _ = st.GetUploadSession(ctx, sess.ID)
	if got.ReceivedBytes != 8 {
		t.Fatalf("重复分片后累计字节 = %d，期望仍为 8", got.ReceivedBytes)
	}
	// 分片变小：累计应回退
	if err := st.UpsertChunk(ctx, sess.ID, 0, 2, "tmp/chunks/x/0.part"); err != nil {
		t.Fatalf("UpsertChunk 回退: %v", err)
	}
	got, _ = st.GetUploadSession(ctx, sess.ID)
	if got.ReceivedBytes != 6 {
		t.Fatalf("分片变小后累计 = %d，期望 6", got.ReceivedBytes)
	}

	chunks, err := st.ListChunks(ctx, sess.ID)
	if err != nil || len(chunks) != 2 {
		t.Fatalf("ListChunks = %d, err=%v", len(chunks), err)
	}
	if chunks[0].Idx != 0 || chunks[1].Idx != 1 {
		t.Fatalf("分片应按序号升序返回")
	}
	if _, err := st.GetChunk(ctx, sess.ID, 0); err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if _, err := st.GetChunk(ctx, sess.ID, 9); err != ErrNotFound {
		t.Fatalf("不存在的分片应返回 ErrNotFound")
	}

	// 重算累计
	if err := st.RecomputeReceived(ctx, sess.ID); err != nil {
		t.Fatalf("RecomputeReceived: %v", err)
	}
	got, _ = st.GetUploadSession(ctx, sess.ID)
	if got.ReceivedBytes != 6 {
		t.Fatalf("重算后累计 = %d，期望 6", got.ReceivedBytes)
	}

	// 分片路径集合（一致性扫描用）
	cp, err := st.AllChunkRelPaths(ctx)
	if err != nil || len(cp) != 2 {
		t.Fatalf("AllChunkRelPaths = %d, err=%v", len(cp), err)
	}

	// 会话完成（CAS + file_id 记录）
	if first, err := st.FinishUploadSession(ctx, sess.ID, 12345); err != nil || !first {
		t.Fatalf("FinishUploadSession = %v, err=%v", first, err)
	}
	got, _ = st.GetUploadSession(ctx, sess.ID)
	if got.Status != model.UploadDone {
		t.Fatalf("会话状态 = %q，期望 done", got.Status)
	}
	if got.FileID == nil || *got.FileID != 12345 {
		t.Fatalf("会话未记录产物文件 ID: %+v", got.FileID)
	}
	// 第二次应返回 false（仅完成一次）
	if first, err := st.FinishUploadSession(ctx, sess.ID, 999); err != nil || first {
		t.Fatalf("重复 FinishUploadSession 应返回 false，实际 %v, err=%v", first, err)
	}
	// 分片记录清理
	if err := st.DeleteChunks(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteChunks: %v", err)
	}
	if chunks, _ := st.ListChunks(ctx, sess.ID); len(chunks) != 0 {
		t.Fatalf("分片记录应已清空")
	}

	// 僵尸会话与已结束会话查询
	stale, err := st.ListStaleUploads(ctx, time.Now().UTC().Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("ListStaleUploads: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("已完成的会话不应出现在僵尸列表中")
	}
	finished, err := st.ListFinishedSessions(ctx, time.Now().UTC().Add(time.Hour), 100)
	if err != nil || len(finished) != 1 {
		t.Fatalf("ListFinishedSessions = %d, err=%v", len(finished), err)
	}

	// 取消会话
	if err := st.AbortUploadSession(ctx, sess.ID); err != nil {
		t.Fatalf("AbortUploadSession: %v", err)
	}
	got, _ = st.GetUploadSession(ctx, sess.ID)
	if got.Status != model.UploadAborted {
		t.Fatalf("取消后状态 = %q", got.Status)
	}

	// 删除会话（分片级联删除）
	if err := st.DeleteUploadSession(ctx, sess.ID); err != nil {
		t.Fatalf("DeleteUploadSession: %v", err)
	}
	if _, err := st.GetUploadSession(ctx, sess.ID); err != ErrNotFound {
		t.Fatalf("删除后应查不到会话")
	}
}

func TestLogRepositoryAndStats(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	uid := int64(1)

	for i := 0; i < 5; i++ {
		e := &model.LogEntry{
			UserID: &uid, EmployeeNo: "40001", Action: model.ActUpload,
			TargetType: "file", TargetID: itoaTest(int64(i)), Detail: "上传文件",
			IP: "192.168.1.10",
		}
		if err := st.InsertLog(ctx, e); err != nil {
			t.Fatalf("InsertLog: %v", err)
		}
	}
	if err := st.InsertLog(ctx, &model.LogEntry{
		EmployeeNo: "40002", Action: model.ActLogin, Detail: "登录成功",
	}); err != nil {
		t.Fatalf("InsertLog: %v", err)
	}

	list, total, err := st.ListLogs(ctx, LogQuery{Page: 1, PageSize: 10})
	if err != nil || total != 6 {
		t.Fatalf("ListLogs total = %d, err=%v", total, err)
	}
	if list[0].ID < list[len(list)-1].ID {
		t.Fatalf("日志应按时间倒序返回")
	}
	if _, total, _ := st.ListLogs(ctx, LogQuery{Action: model.ActLogin, Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("按动作筛选应命中 1 条")
	}
	if _, total, _ := st.ListLogs(ctx, LogQuery{UserID: uid, Page: 1, PageSize: 10}); total != 5 {
		t.Fatalf("按用户筛选应命中 5 条")
	}
	if _, total, _ := st.ListLogs(ctx, LogQuery{Keyword: "40002", Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("按关键字筛选应命中 1 条")
	}
	from := time.Now().UTC().Add(-time.Hour)
	if _, total, _ := st.ListLogs(ctx, LogQuery{From: &from, Page: 1, PageSize: 10}); total != 6 {
		t.Fatalf("时间过滤应命中全部")
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, total, _ := st.ListLogs(ctx, LogQuery{From: &future, Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("未来时间过滤应命中 0 条")
	}

	actions, err := st.DistinctActions(ctx)
	if err != nil || len(actions) != 2 {
		t.Fatalf("DistinctActions = %v, err=%v", actions, err)
	}
	if recent, err := st.ListRecentLogs(ctx, 3); err != nil || len(recent) != 3 {
		t.Fatalf("ListRecentLogs = %d, err=%v", len(recent), err)
	}

	// 裁剪：删除 1 小时前的日志
	if n, err := st.PurgeLogs(ctx, time.Now().UTC().Add(time.Hour)); err != nil || n != 6 {
		t.Fatalf("PurgeLogs = %d, err=%v", n, err)
	}
	if _, total, _ := st.ListLogs(ctx, LogQuery{Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("裁剪后应为空")
	}

	// 统计
	stats, err := st.Stats(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Users != 0 || stats.Files != 0 {
		t.Fatalf("空库统计应为 0: %+v", stats)
	}
}

func TestNamedLock(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ok1, release1, err := st.TryLock(ctx, "lanfs_test_lock", 0)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if !ok1 {
		t.Fatalf("首次获取锁应成功")
	}

	// 锁被占用时，另一连接以 0 超时应失败（验证多副本只跑一份维护任务）。
	ok2, _, err := st.TryLock(ctx, "lanfs_test_lock", 0)
	if err != nil {
		t.Fatalf("TryLock(2): %v", err)
	}
	if ok2 {
		t.Fatalf("锁已被占用时不应再次获取成功")
	}

	release1()

	ok3, release3, err := st.TryLock(ctx, "lanfs_test_lock", 0)
	if err != nil {
		t.Fatalf("TryLock(3): %v", err)
	}
	if !ok3 {
		t.Fatalf("释放后应能重新获取")
	}
	release3()
}

func TestForeignKeysProtectIntegrity(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	owner := createTestUser(t, st, "50001", "钱七")

	f := &model.File{
		OwnerID: owner.ID, OriginalNam: "x.txt", Ext: ".txt", SizeBytes: 1,
		RelPath: "users/" + itoaTest(owner.ID) + "/1.txt",
		Status:  model.StatusActive, ExpiresAt: time.Now().UTC().AddDate(0, 0, 15),
	}
	if err := st.CreateFile(ctx, f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	// 仍有文件时删除账号应被外键阻止（ON DELETE RESTRICT）。
	if err := st.DeleteUser(ctx, owner.ID); err == nil {
		t.Fatalf("有文件的账号不应能被直接删除")
	}
	if n, err := st.CountFilesByOwner(ctx, owner.ID); err != nil || n != 1 {
		t.Fatalf("CountFilesByOwner = %d, err=%v", n, err)
	}
	if list, err := st.ListFilesByOwner(ctx, owner.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListFilesByOwner = %d, err=%v", len(list), err)
	}

	// 先删文件再删账号应成功。
	if _, err := st.DeleteFile(ctx, f.ID); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if err := st.DeleteUser(ctx, owner.ID); err != nil {
		t.Fatalf("清理文件后应能删除账号: %v", err)
	}

	// 上传会话随账号级联删除。
	owner2 := createTestUser(t, st, "50002", "孙八")
	sess := &model.UploadSession{
		ID: strings.Repeat("e", 32), OwnerID: owner2.ID, OriginalName: "a.bin",
		Ext: ".bin", SizeBytes: 1, ChunkSize: 4, TotalChunks: 1,
		Status: model.UploadUploading, DirRel: "users/" + itoaTest(owner2.ID),
	}
	if err := st.CreateUploadSession(ctx, sess); err != nil {
		t.Fatalf("CreateUploadSession: %v", err)
	}
	if err := st.UpsertChunk(ctx, sess.ID, 0, 1, "tmp/chunks/e/0.part"); err != nil {
		t.Fatalf("UpsertChunk: %v", err)
	}
	if err := st.DeleteUser(ctx, owner2.ID); err != nil {
		t.Fatalf("删除账号应级联删除上传会话: %v", err)
	}
	if _, err := st.GetUploadSession(ctx, sess.ID); err != ErrNotFound {
		t.Fatalf("会话应随账号删除")
	}
}

func createTestUser(t *testing.T, st *Store, employeeNo, name string) *model.User {
	t.Helper()
	ctx := context.Background()
	u := &model.User{
		EmployeeNo: employeeNo, Name: name, Password: "test-hash",
		Role: model.RoleUser, Enabled: true, DirRel: "users/0",
	}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser(%s): %v", employeeNo, err)
	}
	dirRel := "users/" + itoaTest(u.ID)
	if err := st.SetUserDirRel(ctx, u.ID, dirRel); err != nil {
		t.Fatalf("SetUserDirRel: %v", err)
	}
	u.DirRel = dirRel
	return u
}

func itoaTest(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// withDBTag 在 DSN 的库名后追加用途后缀，使各测试包互不干扰。
//
//	root:@tcp(127.0.0.1:3306)/lanfs_it?x=y  →  root:@tcp(127.0.0.1:3306)/lanfs_it_store?x=y
func withDBTag(dsn, tag string) string {
	// 分离参数部分
	base, params := dsn, ""
	if i := strings.Index(dsn, "?"); i >= 0 {
		base, params = dsn[:i], dsn[i:]
	}
	// 库名是最后一个 '/' 之后的部分
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
