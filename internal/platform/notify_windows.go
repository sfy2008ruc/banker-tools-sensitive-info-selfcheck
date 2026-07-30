//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

// Notify 弹出系统消息框。Windows 版以 -H windowsgui 编译，没有控制台，
// 浏览器调起失败等情况必须用弹窗告知用户，否则程序看起来"双击没反应"。
func Notify(title, body string) {
	const mbOK = 0x0
	const mbIconInfo = 0x40
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(title)
	b, _ := syscall.UTF16PtrFromString(body)
	proc.Call(0, uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(t)), mbOK|mbIconInfo)
}
