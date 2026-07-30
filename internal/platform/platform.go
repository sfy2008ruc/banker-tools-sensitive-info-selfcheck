// Package platform 收拢所有操作系统差异：路径展开、回收站、浏览器调起（plan 3.2）。
package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var winEnvRe = regexp.MustCompile(`%([^%]+)%`)

// Expand 展开规则文件中的路径变量：Windows 的 %VAR% 与 Unix 的前缀 ~。
func Expand(path string) string {
	if runtime.GOOS == "windows" {
		return winEnvRe.ReplaceAllStringFunc(path, func(m string) string {
			return os.Getenv(strings.Trim(m, "%"))
		})
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// ExpandAll 展开一组路径并丢弃展开后不存在的目录（如未插的 D/E/F 盘）。
func ExpandAll(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		e := Expand(p)
		if e == "" {
			continue
		}
		if st, err := os.Stat(e); err == nil && st.IsDir() {
			out = append(out, filepath.Clean(e))
		}
	}
	return out
}

// OpenBrowser 用系统默认浏览器打开 url；失败不致命，由调用方打印地址兜底。
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux / 信创
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
