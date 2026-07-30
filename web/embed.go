// Package web 内嵌前端静态文件（plan 3.7：全部资源打进单一可执行文件）。
package web

import "embed"

//go:embed all:dist
var FS embed.FS
