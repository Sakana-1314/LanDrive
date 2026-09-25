package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRelAcceptsValidPaths(t *testing.T) {
	cases := []string{
		"users/12/34.pdf",
		"users/1/a.txt",
		"tmp/chunks/abc123/0.part",
		"users/12/文件名.docx",
	}
	for _, c := range cases {
		got, err := SafeRel(c)
		if err != nil {
			t.Fatalf("SafeRel(%q) 期望通过，实际报错: %v", c, err)
		}
		if got != c {
			t.Fatalf("SafeRel(%q) = %q，期望原样返回", c, got)
		}
	}
}

func TestSafeRelRejectsDangerousPaths(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"/etc/passwd",
		"../secret.txt",
		"users/../../etc/passwd",
		"users//12.txt",
		"users/./12.txt",
		"users\\12\\34.pdf",
		"users/12/34.pdf/",
		"C:/windows/system32",
		"users/12/x\x00.pdf",
		strings.Repeat("a", MaxRelLen+1),
	}
	for _, c := range cases {
		if _, err := SafeRel(c); err == nil {
			t.Fatalf("SafeRel(%q) 期望被拒绝，实际通过", c)
		}
	}
}

// TestUserDirName 覆盖「工号当路径段」的校验。
//
// 工号会直接拼进磁盘路径，所以这里必须比 handler 的宽松校验更严：
// 一旦放过 ".." 或含分隔符的工号，就会写到数据根目录之外。
// TestSanitizeFolderName 覆盖文件夹名清洗。
//
// 文件夹名会被当作**单层路径段**拼进磁盘路径，因此必须：
//   - 去掉斜杠与反斜杠（否则用户输入 "a/b" 会意外造出层级）；
//   - 拒绝 "." / ".."（目录穿越）；
//   - 去掉控制字符与 Windows 保留字符。
func TestSanitizeFolderName(t *testing.T) {
	ok := map[string]string{
		"报表":        "报表",
		"2026 年度":   "2026 年度",
		"a-b_c.txt": "a-b_c.txt",
		"  空格  ":    "空格",
		"a/b":       "b", // 只取最后一段，不造层级
		`a\\b`:      "b",
		"a:b":       "ab", // 冒号是 Windows 保留字符，去掉
		"a*b?c":     "abc",
		"..":        "",
		".":         "",
		"":          "",
		"   ":       "",
		"a.txt.":    "a.txt",
	}
	for in, want := range ok {
		if got := SanitizeFolderName(in); got != want {
			t.Fatalf("SanitizeFolderName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestFolderDirRel 覆盖目录相对路径的生成与穿越防护。
func TestFolderDirRel(t *testing.T) {
	cases := []struct {
		userDir, folderPath, want string
	}{
		{"users/1001", "", "users/1001"},
		{"users/1001", "报表", "users/1001/报表"},
		{"users/1001", "报表/2026", "users/1001/报表/2026"},
		{"users/1001", "/报表/", "users/1001/报表"},
	}
	for _, c := range cases {
		got, err := FolderDirRel(c.userDir, c.folderPath)
		if err != nil {
			t.Fatalf("FolderDirRel(%q,%q) 出错: %v", c.userDir, c.folderPath, err)
		}
		if got != c.want {
			t.Fatalf("FolderDirRel(%q,%q) = %q，期望 %q", c.userDir, c.folderPath, got, c.want)
		}
	}
	// 穿越必须被拒（SafeRel 会挡住 ..）
	for _, bad := range []string{"../etc", "报表/../../etc", "a/../../../b"} {
		if _, err := FolderDirRel("users/1001", bad); err == nil {
			t.Fatalf("FolderDirRel 应拒绝穿越路径 %q", bad)
		}
	}
}

func TestUserDirName(t *testing.T) {
	ok := []string{"1001", "admin", "user_01", "A-9", "a.b", "20001234"}
	for _, in := range ok {
		if _, err := UserDirName(in); err != nil {
			t.Fatalf("UserDirName(%q) 应通过: %v", in, err)
		}
	}
	bad := []string{"", "  ", ".", "..", "a/b", `a\\b`, "../etc", "a b", "工号", "a:b", "a*b"}
	for _, in := range bad {
		if got, err := UserDirName(in); err == nil {
			t.Fatalf("UserDirName(%q) 应被拒绝，实际通过为 %q", in, got)
		}
	}
	// 含空格的工号尤其危险：handler 目前已拒绝，但这里是最后一道关。
	if _, err := UserDirName("100 1"); err == nil {
		t.Fatalf("含空格的工号应被拒绝")
	}
}

func TestFileRelAndChunkRel(t *testing.T) {
	// 目录就是工号：FileRel 接目录相对路径而不是 owner id，
	// 这样存量数据（早期 users/<id>）无需迁移也能继续写自己的目录。
	if got := FileRel("users/1001", 34, ".PDF"); got != "users/1001/34.pdf" {
		t.Fatalf("FileRel = %q", got)
	}
	// 无扩展名
	if got := FileRel("users/1001", 2, ""); got != "users/1001/2" {
		t.Fatalf("FileRel 无扩展名 = %q", got)
	}
	// 非法字符被过滤
	if got := FileRel("users/1001", 2, ".p<n>g"); got != "users/1001/2.png" {
		t.Fatalf("FileRel 过滤非法字符 = %q", got)
	}
	if got := ChunkRel("abc", 7); got != "tmp/chunks/abc/7.part" {
		t.Fatalf("ChunkRel = %q", got)
	}
	if got := UserDirRel("1001"); got != "users/1001" {
		t.Fatalf("UserDirRel = %q", got)
	}
	// 生成的路径必须能通过 SafeRel 校验。
	for _, p := range []string{FileRel("users/1001", 34, ".pdf"), ChunkRel("abc", 7), UserDirRel("1001")} {
		if _, err := SafeRel(p); err != nil {
			t.Fatalf("生成路径 %q 未通过 SafeRel: %v", p, err)
		}
	}
}

func TestNormalizeExt(t *testing.T) {
	cases := map[string]string{
		"pdf":     ".pdf",
		".PDF":    ".pdf",
		"  JPG  ": ".jpg",
		"":        "",
		".":       "",
		".tar.gz": ".tar.gz",
		".中文":     "",
		".a b":    ".ab",
	}
	for in, want := range cases {
		if got := NormalizeExt(in); got != want {
			t.Fatalf("NormalizeExt(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 超长扩展名被截断到 31 字节以内（ext 列为 VARCHAR(32)）。
	long := NormalizeExt("." + strings.Repeat("a", 80))
	if len(long) > 31 {
		t.Fatalf("超长扩展名未被截断: %d 字节", len(long))
	}
}

func TestExtOf(t *testing.T) {
	cases := map[string]string{
		"报表.XLSX":         ".xlsx",
		"a.txt":           ".txt",
		"noext":           "",
		".bashrc":         "",
		"archive.tar.gz":  ".gz",
		"a.":              "",
		`C:\dir\file.PDF`: ".pdf",
		"../../evil.sh":   ".sh",
	}
	for in, want := range cases {
		if got := ExtOf(in); got != want {
			t.Fatalf("ExtOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"normal.txt":          "normal.txt",
		"  空格.txt  ":          "空格.txt",
		"../越权.txt":           "越权.txt",
		`C:\windows\evil.exe`: "evil.exe",
		"a/b/c.txt":           "c.txt",
		"":                    "未命名文件",
		"...":                 "未命名文件",
		"tab\there.txt":       "tabhere.txt",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Fatalf("SanitizeName(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 超长文件名按字节截断，且必须是合法 UTF-8。
	long := SanitizeName(strings.Repeat("中", 200) + ".txt")
	if len(long) > MaxNameLen {
		t.Fatalf("超长文件名未被截断: %d 字节", len(long))
	}
	if !utf8Valid(long) {
		t.Fatalf("截断后不是合法 UTF-8: %q", long)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

func TestSplitJoinName(t *testing.T) {
	base, ext := SplitName("报表.xlsx")
	if base != "报表" || ext != ".xlsx" {
		t.Fatalf("SplitName = (%q, %q)", base, ext)
	}
	if got := JoinName(base, ext); got != "报表.xlsx" {
		t.Fatalf("JoinName = %q", got)
	}
	// 无扩展名
	b2, e2 := SplitName("noext")
	if b2 != "noext" || e2 != "" {
		t.Fatalf("SplitName(noext) = (%q, %q)", b2, e2)
	}
	// 大写扩展名会被规范化
	b3, e3 := SplitName("A.TXT")
	if b3 != "A" || e3 != ".txt" {
		t.Fatalf("SplitName(A.TXT) = (%q, %q)", b3, e3)
	}
}

func TestStorageWriteReadRemove(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !filepath.IsAbs(st.Root()) {
		t.Fatalf("数据根目录应为绝对路径: %q", st.Root())
	}

	dir, err := st.EnsureUserDir("1001")
	if err != nil {
		t.Fatalf("EnsureUserDir: %v", err)
	}
	if dir != "users/1001" {
		t.Fatalf("EnsureUserDir = %q", dir)
	}
	if _, err := os.Stat(filepath.Join(root, "users", "1001")); err != nil {
		t.Fatalf("用户目录未创建: %v", err)
	}
	// tmp/chunks 也应已初始化。
	if _, err := os.Stat(filepath.Join(root, "tmp", "chunks")); err != nil {
		t.Fatalf("分片目录未初始化: %v", err)
	}

	rel := ChunkRel("up1", 0)
	n, err := st.WriteChunk(rel, strings.NewReader("hello world"), 1<<20)
	if err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if n != 11 {
		t.Fatalf("写入字节数 = %d，期望 11", n)
	}
	if !st.Exists(rel) {
		t.Fatalf("文件应存在: %s", rel)
	}

	size, sum, err := st.HashFile(rel)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if size != 11 {
		t.Fatalf("HashFile size = %d", size)
	}
	// "hello world" 的 sha256 是固定值。
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if sum != want {
		t.Fatalf("sha256 = %s，期望 %s", sum, want)
	}

	// 重复写入（幂等覆盖）
	n2, err := st.WriteChunk(rel, strings.NewReader("hi"), 1<<20)
	if err != nil {
		t.Fatalf("WriteChunk 覆盖: %v", err)
	}
	if n2 != 2 {
		t.Fatalf("覆盖写入字节数 = %d", n2)
	}

	if err := st.Remove(rel); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if st.Exists(rel) {
		t.Fatalf("删除后文件仍存在")
	}
	// 再次删除应视为成功（幂等）。
	if err := st.Remove(rel); err != nil {
		t.Fatalf("重复 Remove 应成功: %v", err)
	}
}

func TestStorageWriteChunkRejectsOverLimit(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rel := ChunkRel("up2", 0)
	// limit 为「分片大小 + 1」；写入超过 limit 的内容应被截断到 limit。
	n, err := st.WriteChunk(rel, strings.NewReader(strings.Repeat("x", 100)), 10)
	if err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if n != 10 {
		t.Fatalf("写入字节数 = %d，期望被限制为 10", n)
	}
}

func TestMergeToProducesOrderedFileAndHash(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 造三个分片："AAA" + "BBB" + "CCC"
	parts := []string{"AAA", "BBB", "CCC"}
	var rels []string
	for i, p := range parts {
		rel := ChunkRel("up3", i)
		if _, err := st.WriteChunk(rel, strings.NewReader(p), 1<<20); err != nil {
			t.Fatalf("WriteChunk(%d): %v", i, err)
		}
		rels = append(rels, rel)
	}

	final := FileRel("users/5", 100, ".bin")
	size, sum, err := st.MergeTo(final, rels)
	if err != nil {
		t.Fatalf("MergeTo: %v", err)
	}
	if size != 9 {
		t.Fatalf("合并后大小 = %d，期望 9", size)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(final)))
	if err != nil {
		t.Fatalf("读取合并结果: %v", err)
	}
	if string(data) != "AAABBBCCC" {
		t.Fatalf("合并内容 = %q，期望按分片顺序拼接", string(data))
	}
	if sum != HashBytes([]byte("AAABBBCCC")) {
		t.Fatalf("合并 sha256 与内容不符")
	}
	// 合并中间态文件不应残留。
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(final)) + ".merging"); !os.IsNotExist(err) {
		t.Fatalf("不应残留 .merging 临时文件")
	}
}

func TestMergeToMissingChunkFailsCleanly(t *testing.T) {
	root := t.TempDir()
	st, _ := New(root)
	final := FileRel("users/1", 1, ".bin")
	if _, _, err := st.MergeTo(final, []string{ChunkRel("nope", 0)}); err == nil {
		t.Fatalf("分片缺失时应返回错误")
	}
	// 失败时不应留下最终文件。
	if st.Exists(final) {
		t.Fatalf("合并失败后不应存在最终文件")
	}
}

func TestPruneEmptyDirs(t *testing.T) {
	root := t.TempDir()
	st, _ := New(root)
	if _, err := st.EnsureUserDir("9"); err != nil {
		t.Fatalf("EnsureUserDir: %v", err)
	}
	if err := st.PruneEmptyDirs("users/9"); err != nil {
		t.Fatalf("PruneEmptyDirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "users", "9")); !os.IsNotExist(err) {
		t.Fatalf("空目录应被删除")
	}
	// 数据根目录本身必须保留。
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("数据根目录不应被删除: %v", err)
	}
	// 非空目录不应被删除。
	if _, err := st.EnsureUserDir("10"); err != nil {
		t.Fatalf("EnsureUserDir: %v", err)
	}
	rel := FileRel("users/10", 1, ".txt")
	if _, err := st.WriteChunk(rel, strings.NewReader("x"), 100); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := st.PruneEmptyDirs("users/10"); err != nil {
		t.Fatalf("PruneEmptyDirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "users", "10")); err != nil {
		t.Fatalf("非空目录不应被删除: %v", err)
	}
}

func TestAbsRejectsEscape(t *testing.T) {
	st, _ := New(t.TempDir())
	if _, err := st.Abs("../outside.txt"); err == nil {
		t.Fatalf("应拒绝逃逸路径")
	}
	if _, err := st.Abs("/etc/passwd"); err == nil {
		t.Fatalf("应拒绝绝对路径")
	}
	abs, err := st.Abs("users/1/2.txt")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if !strings.HasPrefix(abs, st.Root()) {
		t.Fatalf("Abs 结果应在根目录内: %q", abs)
	}
}

func TestMIMEFor(t *testing.T) {
	cases := map[string]string{
		".pdf":  "application/pdf",
		".PNG":  "image/png",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".exe":  "application/octet-stream", // 未知类型必须回退
		"":      "application/octet-stream",
	}
	for ext, want := range cases {
		if got := MIMEFor(ext, ""); got != want {
			t.Fatalf("MIMEFor(%q) = %q，期望 %q", ext, got, want)
		}
	}
}

func TestIsInlinePreviewableExcludesDangerousTypes(t *testing.T) {
	// SVG / HTML 可执行脚本，绝不能内联。
	for _, ext := range []string{".svg", ".html", ".htm", ".js", ".xml"} {
		if IsInlinePreviewable(ext) {
			t.Fatalf("%s 不应允许内联预览（存在脚本执行风险）", ext)
		}
	}
	for _, ext := range []string{".png", ".jpg", ".pdf", ".mp4", ".mp3"} {
		if !IsInlinePreviewable(ext) {
			t.Fatalf("%s 应允许内联预览", ext)
		}
	}
}

func TestIsText(t *testing.T) {
	if !IsText(".txt") || !IsText(".JSON") || !IsText(".log") {
		t.Fatalf("文本类型判定错误")
	}
	if IsText(".png") || IsText(".docx") {
		t.Fatalf("非文本类型被误判")
	}
}

func TestWalkFilesAndDirSize(t *testing.T) {
	root := t.TempDir()
	st, _ := New(root)
	for i := 0; i < 3; i++ {
		rel := FileRel("users/3", int64(i), ".txt")
		if _, err := st.WriteChunk(rel, strings.NewReader("abcde"), 100); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
	}
	count, bytes, err := st.DirSize("users/3")
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if count != 3 || bytes != 15 {
		t.Fatalf("DirSize = (%d, %d)，期望 (3, 15)", count, bytes)
	}

	var seen []string
	if err := st.WalkFiles("users", func(rel string, _ os.FileInfo) error {
		seen = append(seen, rel)
		return nil
	}); err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("遍历到 %d 个文件，期望 3", len(seen))
	}
	// 前缀不存在的目录应安全返回 nil。
	if err := st.WalkFiles("nonexistent", func(string, os.FileInfo) error { return nil }); err != nil {
		t.Fatalf("遍历不存在的目录应返回 nil: %v", err)
	}
}

func TestHashIntoMatchesHashFile(t *testing.T) {
	root := t.TempDir()
	st, _ := New(root)
	var rels []string
	for i, part := range []string{"abc", "defg", "h"} {
		rel := ChunkRel("hu", i)
		if _, err := st.WriteChunk(rel, strings.NewReader(part), 100); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		rels = append(rels, rel)
	}
	h, err := st.NewHasher()
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	for _, rel := range rels {
		if err := st.HashInto(h, rel); err != nil {
			t.Fatalf("HashInto: %v", err)
		}
	}
	if h.Hex() != HashBytes([]byte("abcdefgh")) {
		t.Fatalf("流式哈希与整体哈希不一致")
	}
}
