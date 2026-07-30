package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix 用例")
	}
	home, _ := os.UserHomeDir()
	if got := Expand("~/桌面"); got != filepath.Join(home, "桌面") {
		t.Fatalf("Expand(~/桌面) = %s", got)
	}
	if got := Expand("~"); got != home {
		t.Fatalf("Expand(~) = %s", got)
	}
	if got := Expand("/media"); got != "/media" {
		t.Fatalf("绝对路径不应被改写: %s", got)
	}
}

func TestExpandAllDropsMissing(t *testing.T) {
	dir := t.TempDir()
	out := ExpandAll([]string{dir, filepath.Join(dir, "不存在的盘")})
	if len(out) != 1 || out[0] != filepath.Clean(dir) {
		t.Fatalf("应只保留存在的目录: %v", out)
	}
}

func TestMovePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	os.WriteFile(src, []byte("hello"), 0o644)
	if err := MovePath(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("移动后源文件应消失")
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "hello" {
		t.Fatalf("目标内容错误: %s, %v", data, err)
	}
}
