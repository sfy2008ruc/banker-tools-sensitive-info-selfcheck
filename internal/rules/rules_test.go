package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFallsBack(t *testing.T) {
	r := Load(t.TempDir())
	if !r.BuiltinFallback {
		t.Fatal("缺失 rules.json 时应 fallback 到内置规则")
	}
	if r.Version != Default().Version {
		t.Fatalf("fallback 版本应为内置版本，got %s", r.Version)
	}
}

func TestLoadCorruptFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rules.json"), []byte("{not json"), 0o644)
	r := Load(dir)
	if !r.BuiltinFallback {
		t.Fatal("损坏的 rules.json 应 fallback 到内置规则")
	}
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	content := `{"version":"202608","keywords":["合同"],"extensions":["pdf",".JPG"],
		"hotDirs":{"linux":["~/桌面"]},"scanRoots":{"linux":["~"]},"excludeDirs":{"linux":["/proc"]}}`
	os.WriteFile(filepath.Join(dir, "rules.json"), []byte(content), 0o644)
	r := Load(dir)
	if r.BuiltinFallback {
		t.Fatal("合法 rules.json 不应 fallback")
	}
	if r.Version != "202608" {
		t.Fatalf("version got %s", r.Version)
	}
	if r.MinImageSizeKB != 30 || r.MaxFileSizeMB != 500 {
		t.Fatal("未填写的阈值应取默认值")
	}
	res := r.ForOS("linux")
	// 后缀应归一化为小写且带点
	if len(res.Extensions) != 2 || res.Extensions[0] != ".pdf" || res.Extensions[1] != ".jpg" {
		t.Fatalf("后缀归一化错误: %v", res.Extensions)
	}
}

func TestForOSDarwinUsesLinux(t *testing.T) {
	r := Default()
	darwin := r.ForOS("darwin")
	linux := r.ForOS("linux")
	if len(darwin.ScanRoots) == 0 || darwin.ScanRoots[0] != linux.ScanRoots[0] {
		t.Fatal("darwin 应复用 linux 规则组")
	}
	win := r.ForOS("windows")
	if win.ScanRoots[0] != `%USERPROFILE%` {
		t.Fatalf("windows 扫描根错误: %v", win.ScanRoots)
	}
}
