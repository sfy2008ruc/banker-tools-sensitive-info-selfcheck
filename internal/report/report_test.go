package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"selfcheck/internal/quarantine"
)

func TestGenerate(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "身份证.jpg")
	os.WriteFile(src, []byte("x"), 0o644)
	q, err := quarantine.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	q.MoveIn([]quarantine.MoveInFile{{Path: src, MatchedRules: []string{"keyword:身份证"}}})

	html, path, err := Generate(q, Stats{
		AppVersion: "v1.0.0", RuleVersion: "202607",
		ScanRoots: []string{home}, DirsScanned: 10, Found: 1, ScanElapsedS: 1.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	for _, want := range []string{"电脑敏感文件自查报告", "202607", "身份证.jpg", "keyword:身份证", "待处理 1", "未上传任何数据"} {
		if !strings.Contains(s, want) {
			t.Fatalf("报告缺少关键内容 %q", want)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("报告应落盘到 reports/ 目录")
	}
	if !strings.Contains(path, filepath.Join(quarantine.DirName, "reports")) {
		t.Fatalf("落盘位置错误: %s", path)
	}
}
