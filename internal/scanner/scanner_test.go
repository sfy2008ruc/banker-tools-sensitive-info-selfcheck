package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"selfcheck/internal/rules"
)

// buildFixture 构造样本目录：
//
//	root/
//	├── 桌面/                      （热点目录）
//	│   ├── 张三身份证.jpg          40KB → 双命中 high
//	│   ├── 普通照片.jpg            40KB → 仅热点后缀 normal
//	│   ├── 小图标.png              1KB  → 小图片过滤
//	│   └── 说明.txt                     → 不命中
//	├── 工作/
//	│   ├── 李四借款合同.pdf              → 仅敏感词（非热点目录）normal
//	│   └── 会议纪要.pdf                  → 后缀命中但非热点目录 → 不命中
//	└── 排除区/
//	    └── 王五合同.pdf                  → 位于排除目录 → 不命中
func buildFixture(t *testing.T) (root string, res *rules.Resolved) {
	t.Helper()
	root = t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("桌面/张三身份证.jpg", 40*1024)
	mk("桌面/普通照片.jpg", 40*1024)
	mk("桌面/小图标.png", 1024)
	mk("桌面/说明.txt", 100)
	mk("工作/李四借款合同.pdf", 2048)
	mk("工作/会议纪要.pdf", 2048)
	mk("排除区/王五合同.pdf", 2048)

	res = &rules.Resolved{
		Version:        "test",
		Keywords:       []string{"身份证", "合同"},
		Extensions:     []string{".jpg", ".png", ".pdf"},
		HotDirs:        []string{filepath.Join(root, "桌面")},
		ScanRoots:      []string{root},
		ExcludeDirs:    []string{filepath.Join(root, "排除区")},
		MinImageSizeKB: 30,
		MaxFileSizeMB:  500,
	}
	return root, res
}

func scanAndWait(t *testing.T, s *Scanner, res *rules.Resolved) map[string]Result {
	t.Helper()
	if !s.Start(res, nil, func(p []string) []string { return p }) {
		t.Fatal("Start 应成功")
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Progress().Status == "running" {
		if time.Now().After(deadline) {
			t.Fatal("扫描超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
	out := map[string]Result{}
	for _, r := range s.Results() {
		out[r.Name] = r
	}
	return out
}

func TestScanMatching(t *testing.T) {
	_, res := buildFixture(t)
	got := scanAndWait(t, New(), res)

	if len(got) != 3 {
		t.Fatalf("应命中 3 个文件，got %d: %v", len(got), keys(got))
	}
	// 双命中 → high
	r := got["张三身份证.jpg"]
	if r.Risk != "high" || len(r.MatchedRules) != 2 {
		t.Fatalf("张三身份证.jpg 应为双命中 high: %+v", r)
	}
	// 仅热点后缀 → normal
	if got["普通照片.jpg"].Risk != "normal" {
		t.Fatalf("普通照片.jpg 应为 normal: %+v", got["普通照片.jpg"])
	}
	// 仅敏感词（非热点目录）→ normal
	if got["李四借款合同.pdf"].Risk != "normal" {
		t.Fatalf("李四借款合同.pdf 应为 normal: %+v", got["李四借款合同.pdf"])
	}
	// 非热点目录纯后缀、不足体积图片、排除目录均不命中
	for _, absent := range []string{"会议纪要.pdf", "小图标.png", "王五合同.pdf", "说明.txt"} {
		if _, ok := got[absent]; ok {
			t.Fatalf("%s 不应命中", absent)
		}
	}
	// 高风险置顶
	s2 := New()
	if !s2.Start(res, nil, func(p []string) []string { return p }) {
		t.Fatal("Start 应成功")
	}
	for s2.Progress().Status == "running" {
		time.Sleep(10 * time.Millisecond)
	}
	if rs := s2.Results(); rs[0].Risk != "high" {
		t.Fatalf("高风险应置顶: %+v", rs[0])
	}
}

func TestScanRejectsConcurrent(t *testing.T) {
	_, res := buildFixture(t)
	s := New()
	s.mu.Lock()
	s.progress.Status = "running"
	s.mu.Unlock()
	if s.Start(res, nil, func(p []string) []string { return p }) {
		t.Fatal("扫描中不应允许再次 Start")
	}
}

func TestCancel(t *testing.T) {
	_, res := buildFixture(t)
	s := New()
	if !s.Start(res, nil, func(p []string) []string { return p }) {
		t.Fatal("Start 应成功")
	}
	s.Cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := s.Progress().Status
		if st == "cancelled" || st == "done" {
			break // 小样本可能在取消前已完成，两者皆可接受
		}
		if time.Now().After(deadline) {
			t.Fatal("取消后未收敛")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHasPath(t *testing.T) {
	root, res := buildFixture(t)
	s := New()
	scanAndWait(t, s, res)
	if !s.HasPath(filepath.Join(root, "桌面", "张三身份证.jpg")) {
		t.Fatal("HasPath 应命中扫描结果内路径")
	}
	if s.HasPath(filepath.Join(root, "工作", "会议纪要.pdf")) {
		t.Fatal("HasPath 不应命中结果之外的路径")
	}
}

func keys(m map[string]Result) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
