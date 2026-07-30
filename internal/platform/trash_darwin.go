//go:build darwin

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MoveToTrash（macOS，仅供开发机验证使用，不随包分发）：移入 ~/.Trash，重名加时间戳。
func MoveToTrash(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return err
	}
	base := filepath.Base(path)
	dst := filepath.Join(trash, base)
	if _, err := os.Lstat(dst); err == nil {
		ext := filepath.Ext(base)
		dst = filepath.Join(trash, fmt.Sprintf("%s-%s%s",
			base[:len(base)-len(ext)], time.Now().Format("150405.000"), ext))
	}
	return movePath(path, dst)
}
