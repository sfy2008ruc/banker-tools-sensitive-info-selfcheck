package platform

import (
	"fmt"
	"io"
	"os"
)

// MovePath 移动文件：优先 rename（同盘瞬时），跨盘退化为 copy + 校验大小 + 删源；
// 任何失败均保留源文件（spec 6.3）。quarantine 的移入/还原与 Trash 实现共用此函数。
func MovePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	if n != srcInfo.Size() {
		os.Remove(dst)
		return fmt.Errorf("复制校验失败：源 %d 字节，目标 %d 字节", srcInfo.Size(), n)
	}
	return os.Remove(src)
}

func movePath(src, dst string) error { return MovePath(src, dst) }
