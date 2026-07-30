package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenBrowser 打开自查页面。优先使用 Chrome（业务系统普遍已装，渲染行为可预期），
// 其次 Edge/Chromium，最后才交给系统默认程序——避免 Windows 未设默认浏览器时
// 弹出"你要如何打开"选择框（V1.2.1 用户反馈）。
func OpenBrowser(url string) error {
	if path := findChrome(); path != "" {
		if err := exec.Command(path, url).Start(); err == nil {
			return nil
		}
	}
	return openWithDefault(url)
}

// findChrome 返回本机 Chrome/Chromium 系浏览器的可执行文件路径，找不到返回空串。
func findChrome() string {
	switch runtime.GOOS {
	case "windows":
		var candidates []string
		for _, base := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LocalAppData"),
		} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	case "darwin":
		for _, c := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	default: // linux / 信创：按常见二进制名在 PATH 中查找
		for _, name := range []string{
			"google-chrome", "google-chrome-stable", "chrome",
			"chromium", "chromium-browser",
			"microsoft-edge", "microsoft-edge-stable",
		} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
	}
	return ""
}

// openWithDefault 交给系统默认程序打开（兜底）。
func openWithDefault(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
