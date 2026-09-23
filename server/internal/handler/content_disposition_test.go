package handler

import (
	"strings"
	"testing"
)

func TestContentDispositionKeepsUTF8Name(t *testing.T) {
	got := contentDisposition("attachment", "季度报表.xlsx", ".xlsx")
	if !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("缺少 disposition 类型: %q", got)
	}
	// RFC 5987 的 filename* 必须能还原原始中文名
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("缺少 RFC 5987 文件名: %q", got)
	}
	if !strings.Contains(got, "%E5%AD%A3%E5%BA%A6%E6%8A%A5%E8%A1%A8.xlsx") {
		t.Fatalf("中文名未正确编码: %q", got)
	}
	// 不能出现裸的中文（老浏览器会乱码）
	if strings.Contains(got, "季度报表") {
		t.Fatalf("filename= 中不应出现裸中文: %q", got)
	}
}

func TestContentDispositionInlineForPreview(t *testing.T) {
	got := contentDisposition("inline", "photo.png", ".png")
	if !strings.HasPrefix(got, "inline;") {
		t.Fatalf("预览应为 inline: %q", got)
	}
}

// TestAsciiFallbackNeverProducesExtensionOnly 是针对真实缺陷的回归测试：
// 纯中文文件名曾被压成 filename=".txt"（只剩扩展名），
// 老浏览器下载后得到没有主名的畸形文件。
func TestAsciiFallbackNeverProducesExtensionOnly(t *testing.T) {
	cases := []struct {
		name, ext, wantStem string
	}{
		{"季度报表.xlsx", ".xlsx", "download"},
		{"报告.pdf", ".pdf", "download"},
		{"纯中文.txt", ".txt", "download"},
		{"🎉🎉.png", ".png", "download"},
		{"report.xlsx", ".xlsx", "report"},
		{"2024 年度 Report.xlsx", ".xlsx", "2024 Report"},
		{"混合 mixed 名称.docx", ".docx", "mixed"},
	}
	for _, c := range cases {
		got := asciiFallbackName(c.name, c.ext)
		if got == "" {
			t.Fatalf("asciiFallbackName(%q) 返回空", c.name)
		}
		if strings.HasPrefix(got, ".") {
			t.Fatalf("asciiFallbackName(%q) = %q，主名不能为空（只有扩展名）", c.name, got)
		}
		if !strings.HasSuffix(got, c.ext) {
			t.Fatalf("asciiFallbackName(%q) = %q，应保留扩展名 %s", c.name, got, c.ext)
		}
		stem := strings.TrimSuffix(got, c.ext)
		if stem == "" {
			t.Fatalf("asciiFallbackName(%q) = %q，主名被清空", c.name, got)
		}
		if stem != c.wantStem {
			t.Fatalf("asciiFallbackName(%q) 主名 = %q，期望 %q", c.name, stem, c.wantStem)
		}
		// 结果必须是纯 ASCII
		for _, r := range got {
			if r > 127 {
				t.Fatalf("asciiFallbackName(%q) = %q 含非 ASCII 字符", c.name, got)
			}
		}
	}
}

func TestAsciiFallbackNoExtension(t *testing.T) {
	got := asciiFallbackName("说明文档", "")
	if got != "download" {
		t.Fatalf("无扩展名的纯中文名应回退为 download，实际 %q", got)
	}
	if strings.Contains(got, ".") {
		t.Fatalf("无扩展名时不应出现点: %q", got)
	}
}

func TestAsciiFallbackEscapesDangerousInput(t *testing.T) {
	// 引号、反斜杠、路径分隔符不得进入 Content-Disposition（防头注入）
	got := asciiFallbackName(`evil"; rm -rf / \ path/../../x.txt`, ".txt")
	for _, bad := range []string{`"`, "\\", "/", ";"} {
		if strings.Contains(got, bad) {
			t.Fatalf("回退名 %q 含危险字符 %q", got, bad)
		}
	}
}
