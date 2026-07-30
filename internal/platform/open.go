package platform

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenPath 用系统默认程序打开文件（V1.1：结果页"打开文件"）。
func OpenPath(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// RevealPath 在文件管理器中定位文件（V1.1：结果页"打开所在文件夹"）。
func RevealPath(path string) error {
	switch runtime.GOOS {
	case "windows":
		// explorer /select, 定位并选中文件
		return exec.Command("explorer", "/select,"+path).Start()
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	default:
		// xdg-open 无定位语义，退化为打开所在目录
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}
