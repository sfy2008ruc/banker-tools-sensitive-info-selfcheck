//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

// MoveToTrash 通过 shell32 SHFileOperationW 将文件移入 Windows 回收站（spec 6.3）。
// 仅用标准库 syscall 直调，不引入 x/sys 依赖。
func MoveToTrash(path string) error {
	const (
		foDelete          = 3
		fofAllowUndo      = 0x40
		fofNoConfirmation = 0x10
		fofSilent         = 0x4
		fofNoErrorUI      = 0x400
	)

	// pFrom 要求双 NUL 结尾的 UTF-16 串
	u16, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	u16 = append(u16, 0)

	// SHFILEOPSTRUCTW（64 位布局，字段对齐由 Go struct 保证）
	type shFileOpStruct struct {
		hwnd                  uintptr
		wFunc                 uint32
		pFrom                 *uint16
		pTo                   *uint16
		fFlags                uint16
		fAnyOperationsAborted int32
		hNameMappings         uintptr
		lpszProgressTitle     *uint16
	}
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &u16[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("SHFileOperationW")
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&op)))
	if ret != 0 {
		return fmt.Errorf("SHFileOperationW 失败，返回码 %d", ret)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("回收站操作被中止")
	}
	return nil
}
