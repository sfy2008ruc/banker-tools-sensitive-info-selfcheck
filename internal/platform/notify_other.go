//go:build !windows

package platform

import "log"

// Notify 在有控制台的平台上打印到日志即可。
func Notify(title, body string) {
	log.Println(title + "：" + body)
}
