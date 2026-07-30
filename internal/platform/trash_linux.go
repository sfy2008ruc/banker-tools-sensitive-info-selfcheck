//go:build linux

package platform

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// MoveToTrash 按 FreeDesktop Trash 规范将文件移入用户回收站（spec 6.3）：
// 文件移入 ~/.local/share/Trash/files/，并写 info/<名>.trashinfo 记录原路径与时间。
// 麒麟/统信桌面均遵循该规范，回收站里可见并可再恢复。
func MoveToTrash(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trash := filepath.Join(home, ".local", "share", "Trash")
	filesDir := filepath.Join(trash, "files")
	infoDir := filepath.Join(trash, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return err
	}

	base := filepath.Base(path)
	name := base
	// 重名加序号，直到 files/ 与 info/ 双双无冲突
	for i := 1; ; i++ {
		_, e1 := os.Lstat(filepath.Join(filesDir, name))
		_, e2 := os.Lstat(filepath.Join(infoDir, name+".trashinfo"))
		if os.IsNotExist(e1) && os.IsNotExist(e2) {
			break
		}
		ext := filepath.Ext(base)
		name = fmt.Sprintf("%s.%d%s", base[:len(base)-len(ext)], i, ext)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		url.PathEscape(abs), time.Now().Format("2006-01-02T15:04:05"))
	if err := os.WriteFile(filepath.Join(infoDir, name+".trashinfo"), []byte(info), 0o600); err != nil {
		return err
	}
	if err := movePath(path, filepath.Join(filesDir, name)); err != nil {
		os.Remove(filepath.Join(infoDir, name+".trashinfo"))
		return err
	}
	return nil
}
