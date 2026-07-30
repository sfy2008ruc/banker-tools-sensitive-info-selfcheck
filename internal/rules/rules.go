// Package rules 负责扫描规则的定义、外置 rules.json 加载与内置默认规则兜底（spec 5.1）。
package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Rules 与 rules.json 一一对应。
type Rules struct {
	Version        string              `json:"version"`
	Keywords       []string            `json:"keywords"`
	Extensions     []string            `json:"extensions"`
	HotDirs        map[string][]string `json:"hotDirs"`
	ScanRoots      map[string][]string `json:"scanRoots"`
	ExcludeDirs    map[string][]string `json:"excludeDirs"`
	MinImageSizeKB int64               `json:"minImageSizeKB"`
	MaxFileSizeMB  int64               `json:"maxFileSizeMB"`

	// BuiltinFallback 标记本规则来自内置默认（外置文件缺失或损坏），页面展示时附注。
	BuiltinFallback bool   `json:"-"`
	FallbackReason  string `json:"-"`
}

// Resolved 是按当前操作系统取出的扁平规则，路径尚未展开（展开交给 platform.Expand）。
type Resolved struct {
	Version        string
	Keywords       []string
	Extensions     []string // 均为小写、含点
	HotDirs        []string
	ScanRoots      []string
	ExcludeDirs    []string
	MinImageSizeKB int64
	MaxFileSizeMB  int64
	BuiltinFallback bool
}

// Default 返回内置默认规则，内容与随包分发的 rules.json 保持一致。
func Default() *Rules {
	return &Rules{
		Version: "202607",
		Keywords: []string{"身份证", "宽表", "证", "合同", "执照", "手机", "地址",
			"流水", "房产", "征信", "授信", "客户"},
		Extensions: []string{".doc", ".docx", ".xls", ".xlsx", ".csv",
			".pdf", ".png", ".jpg", ".jpeg", ".bmp", ".zip", ".rar"},
		HotDirs: map[string][]string{
			"windows": {`%USERPROFILE%\Desktop`, `%USERPROFILE%\Downloads`,
				`%USERPROFILE%\Documents`, `%USERPROFILE%\Documents\WeChat Files`,
				`%USERPROFILE%\Documents\Tencent Files`},
			"linux": {"~/桌面", "~/下载", "~/文档", "~/Desktop", "~/Downloads", "~/Documents"},
		},
		ScanRoots: map[string][]string{
			"windows": {`%USERPROFILE%`, `D:\`, `E:\`, `F:\`},
			"linux":   {"~", "/media", "/mnt"},
		},
		ExcludeDirs: map[string][]string{
			"windows": {`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`,
				`%USERPROFILE%\AppData`},
			"linux": {"/proc", "/sys", "/usr", "/opt", "/var", "~/.cache", "~/.config"},
		},
		MinImageSizeKB: 30,
		MaxFileSizeMB:  500,
	}
}

// Load 读取 dir 目录下的 rules.json；缺失或损坏时返回内置默认规则并记录原因。
func Load(dir string) *Rules {
	path := filepath.Join(dir, "rules.json")
	data, err := os.ReadFile(path)
	if err != nil {
		r := Default()
		r.BuiltinFallback = true
		r.FallbackReason = "未找到 rules.json，使用内置规则"
		return r
	}
	var r Rules
	if err := json.Unmarshal(data, &r); err != nil || r.Version == "" || len(r.Keywords)+len(r.Extensions) == 0 {
		d := Default()
		d.BuiltinFallback = true
		d.FallbackReason = "rules.json 解析失败，使用内置规则"
		return d
	}
	if r.MinImageSizeKB <= 0 {
		r.MinImageSizeKB = 30
	}
	if r.MaxFileSizeMB <= 0 {
		r.MaxFileSizeMB = 500
	}
	return &r
}

// ForOS 取出对应操作系统的扁平规则。goos 为 runtime.GOOS；darwin（开发机）复用 linux 组。
func (r *Rules) ForOS(goos string) *Resolved {
	key := "linux"
	if goos == "windows" {
		key = "windows"
	}
	exts := make([]string, 0, len(r.Extensions))
	for _, e := range r.Extensions {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if e != "" {
			exts = append(exts, e)
		}
	}
	return &Resolved{
		Version:         r.Version,
		Keywords:        r.Keywords,
		Extensions:      exts,
		HotDirs:         r.HotDirs[key],
		ScanRoots:       r.ScanRoots[key],
		ExcludeDirs:     r.ExcludeDirs[key],
		MinImageSizeKB:  r.MinImageSizeKB,
		MaxFileSizeMB:   r.MaxFileSizeMB,
		BuiltinFallback: r.BuiltinFallback,
	}
}
