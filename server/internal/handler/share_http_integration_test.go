// Package handler_test 用**真实路由**验证分享的公开访问边界。
//
// 放在外部测试包（handler_test）而不是 handler 内：本测试需要装配 router，
// 而 router 依赖 handler，同包内导入会形成 import cycle。
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"lan-drive/internal/auth"
	"lan-drive/internal/config"
	"lan-drive/internal/files"
	"lan-drive/internal/folders"
	"lan-drive/internal/handler"
	"lan-drive/internal/maintain"
	"lan-drive/internal/model"
	"lan-drive/internal/router"
	"lan-drive/internal/settings"
	"lan-drive/internal/shares"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
	"lan-drive/internal/upload"
)

// 这一组测试跑的是**真实路由**（含中间件），不是直接调 handler 方法，
// 因为要验证的重点恰恰是"哪些接口不需要登录" —— 那是路由装配层面的性质。

type httpEnv struct {
	engine *gin.Engine
	st     *store.Store
	disk   *storage.Storage
	maint  *maintain.Service
	set    *settings.Service
	user   *model.User
	token  string
}

// timePtr 取时间地址：model.File.ExpiresAt 是 *time.Time（nil = 永久），
// 测试里大多要造"有期限"的文件，用这个helper 免得每处写一个临时变量。
func timePtr(t time.Time) *time.Time { return &t }

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("LANDRIVE_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skipf("未设置 LANDRIVE_TEST_MYSQL_DSN，跳过分享 HTTP 集成测试")
	}
	dsn = withTag(dsn, "handler_share")
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

	u := &model.User{
		EmployeeNo: "90001", Name: "分享者", Password: "h",
		Role: model.RoleUser, Enabled: true, DirRel: "users/90001",
	}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := st.SetUserDirRel(ctx, u.ID, u.DirRel); err != nil {
		t.Fatalf("SetUserDirRel: %v", err)
	}

	tokens := auth.NewTokenManager(strings.Repeat("k", 40), time.Hour)
	filesSvc := files.New(st, disk, set)
	upSvc := upload.New(st, disk, set, filesSvc.Decorate)

	maintSvc := maintain.New(st, disk, set)
	h := handler.New(handler.Deps{
		Store: st, Storage: disk, Settings: set,
		Files: filesSvc, Folders: folders.New(st, disk), Shares: shares.New(st, disk, set),
		Uploads: upSvc, Maintain: maintSvc, Tokens: tokens,
	})
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWTSecret: strings.Repeat("k", 40)}
	engine := router.New(h, cfg)

	tok, _, err := tokens.Issue(u.ID, u.EmployeeNo, u.Role, u.Password)
	if err != nil {
		t.Fatalf("签发令牌: %v", err)
	}
	return &httpEnv{engine: engine, st: st, disk: disk, maint: maintSvc, set: set, user: u, token: tok}
}

// do 发一个请求。token 为空表示**不带 Authorization 头**（模拟外部访客）。
func (e *httpEnv) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, r)
	return w
}

// mkFile 建一条真实落盘的文件。
func (e *httpEnv) mkFile(t *testing.T, name, ext, content string) *model.File {
	t.Helper()
	ctx := context.Background()
	f := &model.File{
		OwnerID: e.user.ID, OriginalNam: name, Ext: ext, SizeBytes: int64(len(content)),
		Mime: storage.MIMEFor(ext, name), SHA256: strings.Repeat("a", 64),
		RelPath: e.user.DirRel + "/tmp", Status: model.StatusActive,
		ExpiresAt: timePtr(time.Now().UTC().AddDate(0, 0, 15)),
	}
	if err := e.st.CreateFile(ctx, f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	rel := storage.FileRel(e.user.DirRel, f.ID, ext)
	if _, err := e.disk.WriteChunk(rel, strings.NewReader(content), 1<<20); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := e.st.FinishFile(ctx, f.ID, rel, strings.Repeat("a", 64), int64(len(content))); err != nil {
		t.Fatalf("FinishFile: %v", err)
	}
	got, err := e.st.GetFileForShare(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFileForShare: %v", err)
	}
	return got
}

// createFolder 通过真实接口建目录（走完整校验与磁盘落盘）。
func (e *httpEnv) createFolder(t *testing.T, name string, parentID int64) *model.Folder {
	t.Helper()
	body := `{"name":"` + name + `","parent_id":` + itoa(parentID) + `}`
	w := e.do(t, "POST", "/api/folders", e.token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("建目录失败 code=%d body=%s", w.Code, w.Body.String())
	}
	var f model.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatalf("解析目录: %v", err)
	}
	return &f
}

// mkFileInFolder 在指定目录里建一条真实落盘的文件。
//
// 与 mkFile 的区别：mkFile 落在一个固定路径，用来测普通文件；
// 目录级"递归到文件"必须让文件真的位于目录树下（rel_path 前缀匹配），
// 否则测出来的是"没递归也通过"。
func (e *httpEnv) mkFileInFolder(t *testing.T, name, ext, content string, folderID int64) *model.File {
	t.Helper()
	ctx := context.Background()
	folder, err := e.st.GetFolder(ctx, folderID)
	if err != nil {
		t.Fatalf("读取目录 %d: %v", folderID, err)
	}
	owner, err := e.st.GetUserByID(ctx, folder.OwnerID)
	if err != nil {
		t.Fatalf("读取属主: %v", err)
	}
	dirRel, err := storage.FolderDirRel(storage.UserDirRel(owner.EmployeeNo), folder.Path)
	if err != nil {
		t.Fatalf("算目录相对路径: %v", err)
	}
	f := &model.File{
		OwnerID: folder.OwnerID, FolderID: folder.ID,
		OriginalNam: name, Ext: ext, SizeBytes: int64(len(content)),
		Mime: storage.MIMEFor(ext, name), SHA256: strings.Repeat("a", 64),
		RelPath: dirRel, Status: model.StatusActive,
		ExpiresAt: timePtr(time.Now().UTC().AddDate(0, 0, 15)),
	}
	if err := e.st.CreateFile(ctx, f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	rel := storage.FileRel(dirRel, f.ID, ext)
	if _, err := e.disk.WriteChunk(rel, strings.NewReader(content), 1<<20); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := e.st.FinishFile(ctx, f.ID, rel, strings.Repeat("a", 64), int64(len(content))); err != nil {
		t.Fatalf("FinishFile: %v", err)
	}
	got, err := e.st.GetFileForShare(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetFileForShare: %v", err)
	}
	return got
}

func (e *httpEnv) createShare(t *testing.T, targetType string, targetID int64, expire string) map[string]any {
	t.Helper()
	body := `{"target_type":"` + targetType + `","target_id":` + itoa(targetID) + expire + `}`
	w := e.do(t, "POST", "/api/shares", e.token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("创建分享失败 code=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析创建分享响应: %v", err)
	}
	return out
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestPublicShareAccessWithoutLogin 是本功能最关键的一条安全断言：
// 分享链接必须**不带任何 Authorization 头**就能打开。
//
// 这条测试同时守住了"不要把 /api/s 误放进鉴权组"这个回归 ——
// 放错组会让外部访客全部拿到 401，分享功能直接失效。
func TestPublicShareAccessWithoutLogin(t *testing.T) {
	e := newHTTPEnv(t)
	f := e.mkFile(t, "公开文件.txt", ".txt", "hello share")
	created := e.createShare(t, "file", f.ID, "")
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("创建分享未返回 token: %v", created)
	}

	// 不带 Authorization
	w := e.do(t, "GET", "/api/s/"+token, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("免登录访问应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("status 应为 ok，实际 %v", res["status"])
	}
	if res["name"] != "公开文件.txt" {
		t.Fatalf("应返回文件名，实际 %v", res["name"])
	}

	// 不带 Authorization 下载
	w = e.do(t, "GET", "/api/s/"+token+"/download", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("免登录下载应返回 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "hello share") {
		t.Fatalf("下载内容不符: %q", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("下载应带 attachment，实际 %q", cd)
	}
}

// TestShareEndpointsRequireAuth 反向断言：
// 管理分享的接口必须仍然要登录，不能因为新增了公开档而误放开。
func TestShareEndpointsRequireAuth(t *testing.T) {
	e := newHTTPEnv(t)
	e.mkFile(t, "受保护.txt", ".txt", "x") // 建个文件，保证下面的 target 有效

	cases := []struct{ method, path, body string }{
		{"GET", "/api/shares", ""},
		{"POST", "/api/shares", `{"target_type":"file","target_id":1}`},
		{"DELETE", "/api/shares/1", ""},
		{"GET", "/api/shares/options", ""},
		{"GET", "/api/folders", ""},
		{"POST", "/api/folders", `{"name":"x"}`},
	}
	for _, c := range cases {
		w := e.do(t, c.method, c.path, "", c.body)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 未登录应返回 401，实际 %d", c.method, c.path, w.Code)
		}
	}
	// 带令牌则可用
	if w := e.do(t, "GET", "/api/shares", e.token, ""); w.Code != http.StatusOK {
		t.Fatalf("已登录查看分享列表应 200，实际 %d", w.Code)
	}
}

// TestShareDeletedFileReturnsClearMessage 验证删除文件后，
// 免登录下载会明确告知"分享的文件已被删除"，而不是笼统的 404。
func TestShareDeletedFileReturnsClearMessage(t *testing.T) {
	e := newHTTPEnv(t)
	f := e.mkFile(t, "会被删.txt", ".txt", "x")
	created := e.createShare(t, "file", f.ID, "")
	token := created["token"].(string)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := e.st.MarkTrashed(ctx, f.ID, now, now.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("MarkTrashed: %v", err)
	}

	// 解析接口应报 deleted
	w := e.do(t, "GET", "/api/s/"+token, "", "")
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "deleted" {
		t.Fatalf("软删后 status 应为 deleted，实际 %v", res["status"])
	}

	// 下载应给出明确文案
	w = e.do(t, "GET", "/api/s/"+token+"/download", "", "")
	if w.Code != http.StatusGone {
		t.Fatalf("已删除应返回 410，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "已被删除") {
		t.Fatalf("应明确提示文件已被删除，实际 %q", w.Body.String())
	}
}

// TestShareInvalidTokenIsNotFound 验证不存在的 token 不会泄漏信息。
func TestShareInvalidTokenIsNotFound(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "GET", "/api/s/0000000000000000000000000000dead", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("解析接口统一返回 200 + status，实际 %d", w.Code)
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "notfound" {
		t.Fatalf("无效 token 应为 notfound，实际 %v", res["status"])
	}
	if w := e.do(t, "GET", "/api/s/0000000000000000000000000000dead/download", "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("无效 token 下载应 404，实际 %d", w.Code)
	}
}

// TestSharePreviewStaysInlineSafe 验证免登录预览仍受内联白名单限制。
//
// 这是安全红线：如果免登录接口把 .svg/.html 当内联下发，
// 等于提供一个"托管并执行任意脚本"的公开入口。
func TestSharePreviewStaysInlineSafe(t *testing.T) {
	e := newHTTPEnv(t)
	// .svg 与 .html 都不在 IsInlinePreviewable 白名单内
	for _, c := range []struct{ name, ext, body string }{
		{"危险.svg", ".svg", `<svg onload="alert(1)"/>`},
		{"危险.html", ".html", `<script>alert(1)</script>`},
	} {
		f := e.mkFile(t, c.name, c.ext, c.body)
		created := e.createShare(t, "file", f.ID, "")
		token := created["token"].(string)
		w := e.do(t, "GET", "/api/s/"+token+"/content", "", "")
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Fatalf("%s 免登录预览必须以附件下发，实际 Content-Disposition=%q", c.ext, cd)
		}
		if ct := w.Header().Get("Content-Type"); strings.Contains(ct, "svg") || strings.Contains(ct, "html") {
			t.Fatalf("%s 不应以可执行类型下发，实际 Content-Type=%q", c.ext, ct)
		}
	}
}

// TestPreviewSizeLimitBlocksBothPaths 守住"可预览文件大小限制"。
//
// 需求：新增可预览文件大小限制，默认 20MB。
//
// 这条测三件事，缺一不可：
//  1. 登录态的 /preview 对超限文件给出 kind=too-large（前端据此改走下载）；
//  2. 免登录分享同样受限 —— 否则公开链接成了绕过预览上限的后门；
//  3. **两个 /content 接口都不能再内联下发超限文件**。只改 /preview 的返回值
//     是不够的：用户可以自己扒出 content_url（或拿一条旧链接）让浏览器去渲染
//     超大文件，"限制"就只剩一句提示。下载能力必须保留（降级为附件）。
func TestPreviewSizeLimitBlocksBothPaths(t *testing.T) {
	e := newHTTPEnv(t)
	ctx := context.Background()

	// 把预览上限压到 1MB，省得在测试里造几十 MB 的字节。
	oneMB := 1
	if _, err := e.set.Update(ctx, settings.Patch{PreviewMaxSizeMB: &oneMB}); err != nil {
		t.Fatalf("设置预览上限: %v", err)
	}

	// 2MB 的 PDF（属于可内联类型，才是真正要拦的对象）。
	big := e.mkFile(t, "大文件.pdf", ".pdf", strings.Repeat("P", 2<<20))
	small := e.mkFile(t, "小文件.pdf", ".pdf", "%PDF-1.4 tiny")

	// --- 1) 登录态 /preview ---
	var info map[string]any
	{
		w := e.do(t, "GET", "/api/files/"+itoa(big.ID)+"/preview", e.token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("preview 失败 code=%d body=%s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatalf("解析 preview 响应: %v", err)
		}
	}
	if got := info["kind"]; got != storage.PreviewTooLarge {
		t.Fatalf("超限文件应返回 kind=%s，实际 %v", storage.PreviewTooLarge, got)
	}
	// 文案要说清原因与做法（异常态不能只给个空提示）。
	note, _ := info["note"].(string)
	if !strings.Contains(note, "下载") {
		t.Fatalf("超限提示应引导用户下载，实际 note=%q", note)
	}
	// 上限内的文件不受影响。
	{
		w := e.do(t, "GET", "/api/files/"+itoa(small.ID)+"/preview", e.token, "")
		var smallInfo map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &smallInfo); err != nil {
			t.Fatalf("解析 preview 响应: %v", err)
		}
		if got := smallInfo["kind"]; got != "pdf" {
			t.Fatalf("上限内的文件应照常预览，实际 kind=%v", got)
		}
	}

	// --- 3a) 登录态 /content：超限必须降级为附件 ---
	{
		w := e.do(t, "GET", "/api/files/"+itoa(big.ID)+"/content", e.token, "")
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Fatalf("超限文件的 content 必须以附件下发，实际 Content-Disposition=%q", cd)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("超限文件的 content 不应给出可渲染 MIME，实际 %q", ct)
		}
	}
	// 上限内的仍应内联（别把正常预览一起关了）。
	{
		w := e.do(t, "GET", "/api/files/"+itoa(small.ID)+"/content", e.token, "")
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "inline") {
			t.Fatalf("上限内的文件应内联，实际 Content-Disposition=%q", cd)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
			t.Fatalf("上限内的 PDF 应以 application/pdf 下发，实际 %q", ct)
		}
	}
	// **下载**不受限制：显式走 /download 仍然拿得到原件。
	{
		w := e.do(t, "GET", "/api/files/"+itoa(big.ID)+"/download", e.token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("下载超限文件应仍然可用，实际 code=%d", w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
			t.Fatalf("下载必须是附件形式")
		}
	}

	// --- 2 & 3b) 免登录分享路径 ---
	created := e.createShare(t, "file", big.ID, "")
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("创建分享未返回 token: %v", created)
	}
	{
		w := e.do(t, "GET", "/api/s/"+token, "", "")
		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("解析分享响应: %v", err)
		}
		if got := res["kind"]; got != storage.PreviewTooLarge {
			t.Fatalf("分享页对超限文件应返回 kind=%s，实际 %v", storage.PreviewTooLarge, got)
		}
	}
	{
		w := e.do(t, "GET", "/api/s/"+token+"/content", "", "")
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Fatalf("免登录 content 对超限文件必须以附件下发，实际 %q", cd)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("免登录 content 不应给出可渲染 MIME，实际 %q", ct)
		}
	}
	{
		w := e.do(t, "GET", "/api/s/"+token+"/download", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("分享下载应仍然可用，实际 code=%d", w.Code)
		}
	}

	// --- 上限设为 0 = 不限制：超限文件恢复预览 ---
	zero := 0
	if _, err := e.set.Update(ctx, settings.Patch{PreviewMaxSizeMB: &zero}); err != nil {
		t.Fatalf("设置预览上限为 0: %v", err)
	}
	{
		w := e.do(t, "GET", "/api/files/"+itoa(big.ID)+"/preview", e.token, "")
		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("解析 preview 响应: %v", err)
		}
		if got := res["kind"]; got != "pdf" {
			t.Fatalf("上限为 0（不限制）时超限文件应照常预览，实际 kind=%v", got)
		}
	}
}

// TestFolderShareEndpoints 覆盖目录分享的创建与访问。
func TestFolderShareEndpoints(t *testing.T) {
	e := newHTTPEnv(t)
	// 建目录
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"报表","parent_id":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("建目录失败 %d: %s", w.Code, w.Body.String())
	}
	var folder model.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &folder); err != nil {
		t.Fatalf("解析目录: %v", err)
	}
	if folder.Path != "报表" {
		t.Fatalf("目录 path 应为 报表，实际 %q", folder.Path)
	}

	// 目录里放一个文件（模拟上传到该目录）
	f := e.mkFile(t, "报表.xlsx", ".xlsx", "data")
	dirRel, err := storage.FolderDirRel(e.user.DirRel, folder.Path)
	if err != nil {
		t.Fatalf("FolderDirRel: %v", err)
	}
	newRel := storage.FileRel(dirRel, f.ID, ".xlsx")
	if _, err := e.disk.WriteChunk(newRel, strings.NewReader("data"), 1<<20); err != nil {
		t.Fatalf("写目录内文件: %v", err)
	}
	if err := e.st.MoveFilesPathPrefix(context.Background(), e.user.ID, e.user.DirRel, dirRel); err != nil {
		t.Fatalf("MoveFilesPathPrefix: %v", err)
	}
	if _, err := e.st.DB().ExecContext(context.Background(),
		`UPDATE files SET folder_id = ? WHERE id = ?`, folder.ID, f.ID); err != nil {
		t.Fatalf("设置 folder_id: %v", err)
	}

	// 目录列表
	w = e.do(t, "GET", "/api/folders?folder_id=0", e.token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("列目录失败 %d", w.Code)
	}
	var listing struct {
		Folders []model.Folder `json:"folders"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listing)
	if len(listing.Folders) != 1 {
		t.Fatalf("应有 1 个子目录，实际 %d", len(listing.Folders))
	}
	if listing.Folders[0].FileCount != 1 {
		t.Fatalf("目录应统计到 1 个文件，实际 %d", listing.Folders[0].FileCount)
	}

	// 分享目录并免登录访问
	created := e.createShare(t, "folder", folder.ID, `,"expire_days":7`)
	token := created["token"].(string)
	w = e.do(t, "GET", "/api/s/"+token, "", "")
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "ok" {
		t.Fatalf("目录分享应可用，实际 %v", res["status"])
	}
	if res["file_count"].(float64) != 1 {
		t.Fatalf("目录分享应报告 1 个文件，实际 %v", res["file_count"])
	}

	// 目录分享下载应得到 zip
	w = e.do(t, "GET", "/api/s/"+token+"/download", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("目录下载应 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "zip") {
		t.Fatalf("目录下载应为 zip，实际 %q", ct)
	}
	if !strings.HasPrefix(w.Body.String(), "PK") {
		t.Fatalf("响应不是有效 zip（应以 PK 开头）")
	}
}

// TestFolderRenameKeepsSharedFileAccessible 验证目录改名后分享仍可用
// —— 这正是"指向记录而不是路径"的价值所在。
func TestFolderRenameKeepsSharedFileAccessible(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"旧名","parent_id":0}`)
	var folder model.Folder
	_ = json.Unmarshal(w.Body.Bytes(), &folder)

	f := e.mkFile(t, "文件.txt", ".txt", "content")
	dirRel, _ := storage.FolderDirRel(e.user.DirRel, "旧名")
	newRel := storage.FileRel(dirRel, f.ID, ".txt")
	_, _ = e.disk.WriteChunk(newRel, strings.NewReader("content"), 1<<20)
	_ = e.st.MoveFilesPathPrefix(context.Background(), e.user.ID, e.user.DirRel, dirRel)

	created := e.createShare(t, "file", f.ID, "")
	token := created["token"].(string)

	// 改名目录
	w = e.do(t, "PATCH", "/api/folders/"+itoa(folder.ID), e.token,
		`{"name":"新名","parent_id":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("目录改名失败 %d: %s", w.Code, w.Body.String())
	}

	// 分享仍可下载，且文件确实在新目录下
	w = e.do(t, "GET", "/api/s/"+token+"/download", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("目录改名后分享应仍可下载，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "content") {
		t.Fatalf("下载内容不符: %q", w.Body.String())
	}
	// 磁盘上新目录存在、旧目录不存在
	if _, err := os.Stat(newDirPath(e, "新名")); err != nil {
		t.Fatalf("新目录应存在: %v", err)
	}
	if _, err := os.Stat(newDirPath(e, "旧名")); !os.IsNotExist(err) {
		t.Fatalf("旧目录应已不存在")
	}
}

func newDirPath(e *httpEnv, name string) string {
	rel := e.user.DirRel + "/" + name
	abs, _ := e.disk.Abs(rel)
	return abs
}

// withTag 给库名加后缀，避免与其他包的测试互相清库。
func withTag(dsn, tag string) string {
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

// TestEmptyFolderShareIsNotReportedAsDeleted 回归一个我实测踩到的错误设计：
// 曾经用"目录里没有文件"来判断目录已被删除，于是**本来就是空的**目录
// 会被误报成"分享的文件夹已被删除"。
// 目录删除是软删除（status=deleted），判断必须依据状态而不是内容多少。
func TestEmptyFolderShareIsNotReportedAsDeleted(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"空目录","parent_id":0}`)
	var folder model.Folder
	_ = json.Unmarshal(w.Body.Bytes(), &folder)

	created := e.createShare(t, "folder", folder.ID, "")
	token := created["token"].(string)

	// 空目录：应正常可用（只是文件数为 0），不能报 deleted
	w = e.do(t, "GET", "/api/s/"+token, "", "")
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "ok" {
		t.Fatalf("空目录分享应可用，实际 status=%v", res["status"])
	}
	if res["file_count"].(float64) != 0 {
		t.Fatalf("空目录 file_count 应为 0，实际 %v", res["file_count"])
	}
}

// TestDeletedFolderShareReportsDeleted 验证删除目录后：
// 分享解析报 deleted（而不是 notfound），且目录名仍能告知。
func TestDeletedFolderShareReportsDeleted(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"待删目录","parent_id":0}`)
	var folder model.Folder
	_ = json.Unmarshal(w.Body.Bytes(), &folder)
	created := e.createShare(t, "folder", folder.ID, "")
	token := created["token"].(string)

	if w := e.do(t, "DELETE", "/api/folders/"+itoa(folder.ID), e.token, ""); w.Code != http.StatusOK {
		t.Fatalf("删目录失败 %d: %s", w.Code, w.Body.String())
	}

	w = e.do(t, "GET", "/api/s/"+token, "", "")
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["status"] != "deleted" {
		t.Fatalf("删目录后应报 deleted，实际 %v", res["status"])
	}
	if res["name"] != "待删目录" {
		t.Fatalf("应保留目录名以便提示，实际 %v", res["name"])
	}

	// 删掉后应能再建同名目录（软删除的墓碑后缀就是为这个）
	w = e.do(t, "POST", "/api/folders", e.token, `{"name":"待删目录","parent_id":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("删除后应能再建同名目录，实际 %d: %s", w.Code, w.Body.String())
	}
}

// TestInvalidExpireReturns400 回归实测缺陷：非法有效期曾返回 500
// （领域错误没有映射到 failErr），客户端拿到的是"服务器错误"而不是"参数不对"。
func TestInvalidExpireReturns400(t *testing.T) {
	e := newHTTPEnv(t)
	f := e.mkFile(t, "x.txt", ".txt", "x")
	w := e.do(t, "POST", "/api/shares", e.token,
		`{"target_type":"file","target_id":`+itoa(f.ID)+`,"expire_days":2}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法有效期应返回 400，实际 %d：%s", w.Code, w.Body.String())
	}
}

// TestUploadIntoTargetFolder 验证带 folder_id 的上传真正落在该目录
// （磁盘真实层级），并且文件列表能按目录过滤出来。
func TestUploadIntoTargetFolder(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"目标目录","parent_id":0}`)
	var folder model.Folder
	_ = json.Unmarshal(w.Body.Bytes(), &folder)

	// 走真实分片上传
	body := `{"file_name":"报表.xlsx","file_size":4,"sha256":"","folder_id":` + itoa(folder.ID) + `}`
	w = e.do(t, "POST", "/api/uploads/init", e.token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("init 失败 %d: %s", w.Code, w.Body.String())
	}
	var sess model.UploadSession
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	// 上传分片
	w = e.do(t, "PUT", "/api/uploads/"+sess.ID+"/chunks/0", e.token, "data")
	if w.Code != http.StatusOK {
		t.Fatalf("上传分片失败 %d: %s", w.Code, w.Body.String())
	}
	w = e.do(t, "POST", "/api/uploads/"+sess.ID+"/complete", e.token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("complete 失败 %d: %s", w.Code, w.Body.String())
	}
	var done struct {
		File model.File `json:"file"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &done)

	// 文件应归属该目录：rel_path 含目录名，folder_id 正确
	if !strings.Contains(done.File.RelPath, "目标目录/") {
		t.Fatalf("文件应落在目标目录下，实际 rel_path=%q", done.File.RelPath)
	}
	if done.File.FolderID != folder.ID {
		t.Fatalf("folder_id 应为 %d，实际 %d", folder.ID, done.File.FolderID)
	}

	// 目录视图能查到
	w = e.do(t, "GET", "/api/files?folder_id="+itoa(folder.ID), e.token, "")
	var list struct {
		Items []model.File `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("目录内应有 1 个文件，实际 %d", len(list.Items))
	}

	// 根目录视图不应包含它
	w = e.do(t, "GET", "/api/files?folder_root=1", e.token, "")
	var rootList struct {
		Items []model.File `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rootList)
	for _, it := range rootList.Items {
		if it.ID == done.File.ID {
			t.Fatalf("根目录视图不应包含已归入子目录的文件")
		}
	}
}

// doRaw 发一个裸二进制请求（分片上传用）。
func doRaw(t *testing.T, e *httpEnv, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, r)
	return w
}

// 验证 /auth/me 的 used_bytes 是真实字节数。
// 这条断言在 CAST 之前是**恒等于 0 的假通过**：go-mysql-server 的 SUM 返回
// float64，Scan 进 int64 报错被 publicWithUsage 忽略，于是永远 0。
func TestAccountUsageReportsRealBytes(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/uploads/init", e.token,
		`{"file_name":"big.txt","file_size":1048576,"sha256":""}`)
	var sess model.UploadSession
	_ = json.Unmarshal(w.Body.Bytes(), &sess)

	// 上传一个大于 1MB 的分片，正好越过 SUM 出错的阈值
	chunk := make([]byte, 1048576)
	for i := range chunk {
		chunk[i] = 'a'
	}
	r := doRaw(t, e, "PUT", "/api/uploads/"+sess.ID+"/chunks/0", e.token, chunk)
	if r.Code != 200 {
		t.Fatalf("分片上传失败 %d: %s", r.Code, r.Body.String())
	}
	w = e.do(t, "POST", "/api/uploads/"+sess.ID+"/complete", e.token, "")
	if w.Code != 200 {
		t.Fatalf("complete 失败 %d: %s", w.Code, w.Body.String())
	}

	w = e.do(t, "GET", "/api/auth/me", e.token, "")
	var me struct {
		User model.User `json:"user"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if me.User.FileCount != 1 {
		t.Fatalf("file_count 应为 1，实际 %d", me.User.FileCount)
	}
	if me.User.UsedBytes != 1048576 {
		t.Fatalf("used_bytes 应为 1048576，实际 %d（SUM 若未 CAST 会静默为 0）", me.User.UsedBytes)
	}
}

// TestFolderDeletePreservesFileBytes 回归一个真实的数据破损缺陷：
// 删除文件夹时曾顺手把磁盘目录 RemoveAll 掉，但文件记录只是**软删**
// （进回收站、管理员可恢复）。结果是回收站里的记录指向不存在的文件，
// "恢复"恢复出来是个坏记录，一致性扫描也报"数据库有、磁盘无"。
// 正确行为：软删记录、**保留字节**，与删单个文件完全一致。
func TestFolderDeletePreservesFileBytes(t *testing.T) {
	e := newHTTPEnv(t)
	w := e.do(t, "POST", "/api/folders", e.token, `{"name":"待删目录","parent_id":0}`)
	var folder model.Folder
	_ = json.Unmarshal(w.Body.Bytes(), &folder)

	// 上传一个文件进该目录
	w = e.do(t, "POST", "/api/uploads/init", e.token,
		`{"file_name":"保留.txt","file_size":4,"sha256":"","folder_id":`+itoa(folder.ID)+`}`)
	var sess model.UploadSession
	_ = json.Unmarshal(w.Body.Bytes(), &sess)
	_ = doRaw(t, e, "PUT", "/api/uploads/"+sess.ID+"/chunks/0", e.token, []byte("data"))
	w = e.do(t, "POST", "/api/uploads/"+sess.ID+"/complete", e.token, "")
	var done struct {
		File model.File `json:"file"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &done)
	if done.File.ID == 0 {
		t.Fatalf("上传失败: %s", w.Body.String())
	}
	abs, err := e.disk.Abs(done.File.RelPath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("上传后文件应在磁盘上: %v", err)
	}

	// 删目录
	if w := e.do(t, "DELETE", "/api/folders/"+itoa(folder.ID), e.token, ""); w.Code != 200 {
		t.Fatalf("删目录失败 %d: %s", w.Code, w.Body.String())
	}

	// 关键断言：记录进回收站，但字节必须还在（否则无法恢复）
	got, err := e.st.GetFileForShare(context.Background(), done.File.ID)
	if err != nil {
		t.Fatalf("记录应保留（软删而非删行）: %v", err)
	}
	if got.Status != model.StatusTrashed {
		t.Fatalf("文件应变为 trashed，实际 %s", got.Status)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("软删后磁盘字节必须保留（否则回收站无法恢复）: %v", err)
	}

	// 一致性扫描不应报"缺失"
	rep, err := e.maint.ScanStorage(context.Background())
	if err != nil {
		t.Fatalf("ScanStorage: %v", err)
	}
	for _, m := range rep.Missing {
		if m == done.File.RelPath {
			t.Fatalf("一致性扫描报缺失（数据破损）: %s", m)
		}
	}
}

// TestPermanentExpiryAndQuota 守住「文件有效期设为永久」这套语义。
//
// 需求：支持将文件有效期设成永久，但要二次确认（前端负责）并提示永久空间
// 只有 100G（可配置）；支持把整个文件夹**递归到文件**设成永久。
//
// 服务端要守的：
//  1. 设永久 = expires_at 置空，返回 permanent=true 且 days_left 归零；
//  2. 到期扫描**不再碰**永久文件（这是"永久"的意义所在，最该测）；
//  3. 永久配额是**全站共享**的，超了要硬拒并给出差额；
//  4. 目录级设置递归到子目录里的文件；
//  5. 改回有期限能重新计时（误设后的补救）。
func TestPermanentExpiryAndQuota(t *testing.T) {
	e := newHTTPEnv(t)
	ctx := context.Background()

	// 配额先设成 1MB，方便造"放不下"的场景（免得真造 100G）。
	oneMB := 1
	if _, err := e.set.Update(ctx, settings.Patch{PermanentQuotaMB: &oneMB}); err != nil {
		t.Fatalf("设置永久配额: %v", err)
	}

	small := e.mkFile(t, "小文件.txt", ".txt", strings.Repeat("s", 1000))
	big := e.mkFile(t, "大文件.txt", ".txt", strings.Repeat("b", 2<<20)) // 2MB > 1MB 配额

	// --- 1) 设永久成功 ---
	{
		w := e.do(t, "PUT", "/api/files/"+itoa(small.ID)+"/permanent", e.token, `{"permanent":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("设永久失败 code=%d body=%s", w.Code, w.Body.String())
		}
		var f model.File
		if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
			t.Fatalf("解析响应: %v", err)
		}
		if !f.Permanent || f.ExpiresAt != nil {
			t.Fatalf("设永久后应 permanent=true 且 expires_at 为空，实际 permanent=%v expires_at=%v", f.Permanent, f.ExpiresAt)
		}
		if f.DaysLeft != 0 {
			t.Fatalf("永久文件 days_left 应为 0，实际 %d", f.DaysLeft)
		}
	}

	// --- 2) 到期扫描不再碰永久文件（把它的到期时间当作"早就过了"也一样） ---
	{
		// 直接把 expires_at 改成过去：永久文件本来就是 NULL，这里改的是
		// 另一条有期限的文件，用来确认扫描仍然正常工作。
		past := time.Now().UTC().Add(-48 * time.Hour)
		expiring := e.mkFile(t, "会过期.txt", ".txt", strings.Repeat("e", 100))
		if _, err := e.st.DB().ExecContext(ctx, `UPDATE files SET expires_at = ? WHERE id = ?`, past, expiring.ID); err != nil {
			t.Fatalf("准备过期数据: %v", err)
		}
		if n := e.maint.RunExpire(ctx); n < 1 {
			t.Fatalf("到期扫描应至少标记 1 个文件，实际 %d", n)
		}
		// 永久文件必须仍是 active —— 若被误清理，"永久"就名不副实。
		after, err := e.st.GetFile(ctx, small.ID)
		if err != nil {
			t.Fatalf("读取永久文件: %v", err)
		}
		if after.Status != model.StatusActive || after.ExpiresAt != nil {
			t.Fatalf("永久文件不应被到期扫描影响，实际 status=%s expires_at=%v", after.Status, after.ExpiresAt)
		}
	}

	// --- 3) 配额硬拒：2MB 的文件放不进只剩 ~1MB 的池 ---
	{
		w := e.do(t, "PUT", "/api/files/"+itoa(big.ID)+"/permanent", e.token, `{"permanent":true}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("超出永久配额应返回 409，实际 %d body=%s", w.Code, w.Body.String())
		}
		// 文案要告诉用户还差多少，否则他不知道怎么办。
		if !strings.Contains(w.Body.String(), "还需") {
			t.Fatalf("配额不足的提示应给出差额，实际 %s", w.Body.String())
		}
		// 拒绝后该文件不应变成永久。
		got, _ := e.st.GetFile(ctx, big.ID)
		if got.ExpiresAt == nil {
			t.Fatalf("配额不足时不应把文件设成永久")
		}
	}

	// --- 4) 目录级递归：子目录里的文件也要被设成永久 ---
	{
		quota := 10
		if _, err := e.set.Update(ctx, settings.Patch{PermanentQuotaMB: &quota}); err != nil {
			t.Fatalf("放宽配额: %v", err)
		}
		// 建 报表/2026 两级目录，文件分别落在两级。
		top := e.createFolder(t, "报表", 0)
		sub := e.createFolder(t, "2026", top.ID)
		inTop := e.mkFileInFolder(t, "顶层.txt", ".txt", strings.Repeat("t", 100), top.ID)
		inSub := e.mkFileInFolder(t, "子层.txt", ".txt", strings.Repeat("u", 100), sub.ID)

		w := e.do(t, "PUT", "/api/folders/"+itoa(top.ID)+"/permanent", e.token, `{"permanent":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("目录设永久失败 code=%d body=%s", w.Code, w.Body.String())
		}
		var res struct {
			Affected  int  `json:"affected"`
			Permanent bool `json:"permanent"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("解析响应: %v", err)
		}
		// 顶层 + 子层各 1 个文件，递归范围应是 2（不含此前手动设永久的那 1 个）。
		if res.Affected != 2 {
			t.Fatalf("目录递归应影响 2 个文件（含子目录），实际 %d", res.Affected)
		}
		for _, id := range []int64{inTop.ID, inSub.ID} {
			got, err := e.st.GetFile(ctx, id)
			if err != nil {
				t.Fatalf("读取文件 %d: %v", id, err)
			}
			if got.ExpiresAt != nil {
				t.Fatalf("目录设永久后文件 %d 仍是有期限（递归没覆盖到子目录？）", id)
			}
		}
	}

	// --- 5) 改回有期限：按当前保留天数重新计时 ---
	{
		w := e.do(t, "PUT", "/api/files/"+itoa(small.ID)+"/permanent", e.token, `{"permanent":false}`)
		if w.Code != http.StatusOK {
			t.Fatalf("改回有期限失败 code=%d body=%s", w.Code, w.Body.String())
		}
		var f model.File
		if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
			t.Fatalf("解析响应: %v", err)
		}
		if f.Permanent || f.ExpiresAt == nil {
			t.Fatalf("改回有期限后应有 expires_at，实际 permanent=%v", f.Permanent)
		}
		// 应约等于 retention_days 天后（默认 15）。
		wantDays := e.set.Get().RetentionDays
		if f.DaysLeft < wantDays-1 || f.DaysLeft > wantDays {
			t.Fatalf("改回有期限后剩余天数应约 %d 天，实际 %d", wantDays, f.DaysLeft)
		}
	}

	// --- 6) 配额为 0 = 关闭永久功能 ---
	{
		zero := 0
		if _, err := e.set.Update(ctx, settings.Patch{PermanentQuotaMB: &zero}); err != nil {
			t.Fatalf("关闭永久功能: %v", err)
		}
		w := e.do(t, "PUT", "/api/files/"+itoa(big.ID)+"/permanent", e.token, `{"permanent":true}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("关闭永久功能后应返回 403，实际 %d body=%s", w.Code, w.Body.String())
		}
		// 关闭状态下改成有期限仍应可用（那是"退回"，不是"申请额度"）。
		if w := e.do(t, "PUT", "/api/files/"+itoa(big.ID)+"/permanent", e.token, `{"permanent":false}`); w.Code != http.StatusOK {
			t.Fatalf("关闭永久功能后改回有期限仍应成功，实际 %d", w.Code)
		}
	}
}
