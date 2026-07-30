// Package scanner 实现并发目录遍历与命中判定（spec 5.2、plan 3.3）。
package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"selfcheck/internal/rules"
)

// Result 是单个命中文件，字段随 /api/results 直接下发。
type Result struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"sizeBytes"`
	ModifiedAt   time.Time `json:"modifiedAt"`
	MatchedRules []string  `json:"matchedRules"`
	Risk         string    `json:"risk"` // high=敏感词+热点后缀双命中（预勾选）；normal=单命中
	Tags         []string  `json:"tags,omitempty"`
}

// Progress 随 /api/scan/progress 下发。
type Progress struct {
	Status          string `json:"status"` // idle | running | done | cancelled
	DirsScanned     int64  `json:"dirsScanned"`
	Found           int64  `json:"found"`
	SkippedNoAccess int64  `json:"skippedNoAccess"`
	ElapsedMS       int64  `json:"elapsedMs"`
	Roots           []string `json:"roots"`
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".bmp": true}

// Scanner 持有一次扫描的进度与结果；同一实例串行复用（进行中拒绝再次 Start）。
type Scanner struct {
	mu       sync.Mutex
	progress Progress
	results  []Result
	cancel   context.CancelFunc
	started  time.Time
}

func New() *Scanner {
	return &Scanner{progress: Progress{Status: "idle"}}
}

// Start 启动扫描；已在扫描中返回 false（server 层转 409）。
// extraExcludes 为规则之外必须排除的目录：暂存区、程序自身目录（spec 5.2）。
func (s *Scanner) Start(res *rules.Resolved, extraExcludes []string, expand func([]string) []string) bool {
	s.mu.Lock()
	if s.progress.Status == "running" {
		s.mu.Unlock()
		return false
	}
	roots := expand(res.ScanRoots)
	excludes := append(expand(res.ExcludeDirs), extraExcludes...)
	hotDirs := expand(res.HotDirs)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = time.Now()
	s.progress = Progress{Status: "running", Roots: roots}
	s.results = nil
	s.mu.Unlock()

	go s.run(ctx, res, roots, excludes, hotDirs)
	return true
}

func (s *Scanner) run(ctx context.Context, res *rules.Resolved, roots, excludes, hotDirs []string) {
	extSet := map[string]bool{}
	for _, e := range res.Extensions {
		extSet[e] = true
	}

	var wg sync.WaitGroup
	seen := map[string]bool{}
	var seenMu sync.Mutex

	for _, root := range roots {
		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if ctx.Err() != nil {
					return filepath.SkipAll
				}
				if err != nil {
					s.mu.Lock()
					s.progress.SkippedNoAccess++
					s.mu.Unlock()
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					if underAny(path, excludes) {
						return filepath.SkipDir
					}
					s.mu.Lock()
					s.progress.DirsScanned++
					s.mu.Unlock()
					return nil
				}
				if !d.Type().IsRegular() {
					return nil
				}
				r, ok := s.match(path, d, res, extSet, hotDirs)
				if !ok {
					return nil
				}
				seenMu.Lock()
				dup := seen[r.Path]
				seen[r.Path] = true
				seenMu.Unlock()
				if dup {
					return nil
				}
				s.mu.Lock()
				s.results = append(s.results, r)
				s.progress.Found++
				s.mu.Unlock()
				return nil
			})
		}(root)
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 高风险置顶，其余按修改时间倒序（对齐现有"按修改时间排查"的习惯，spec P2）
	sort.SliceStable(s.results, func(i, j int) bool {
		if s.results[i].Risk != s.results[j].Risk {
			return s.results[i].Risk == "high"
		}
		return s.results[i].ModifiedAt.After(s.results[j].ModifiedAt)
	})
	s.progress.ElapsedMS = time.Since(s.started).Milliseconds()
	if ctx.Err() != nil {
		s.progress.Status = "cancelled"
	} else {
		s.progress.Status = "done"
	}
}

func (s *Scanner) match(path string, d fs.DirEntry, res *rules.Resolved,
	extSet map[string]bool, hotDirs []string) (Result, bool) {
	name := d.Name()
	ext := strings.ToLower(filepath.Ext(name))

	var matched []string
	for _, kw := range res.Keywords {
		if kw != "" && strings.Contains(name, kw) {
			matched = append(matched, "keyword:"+kw)
		}
	}
	extHot := extSet[ext] && underAny(path, hotDirs)
	if extHot {
		matched = append(matched, "ext:"+ext+"@hotdir")
	}
	if len(matched) == 0 {
		return Result{}, false
	}

	info, err := d.Info()
	if err != nil {
		return Result{}, false
	}
	// 小图片过滤（图标/表情包噪音，spec 5.2）
	if imageExts[ext] && info.Size() < res.MinImageSizeKB*1024 {
		return Result{}, false
	}

	r := Result{
		Path: path, Name: name,
		SizeBytes: info.Size(), ModifiedAt: info.ModTime(),
		MatchedRules: matched, Risk: "normal",
	}
	// 双命中 = 同时存在敏感词命中与热点后缀命中（spec 5.2 高风险判定）
	hasKeyword := false
	for _, m := range matched {
		if strings.HasPrefix(m, "keyword:") {
			hasKeyword = true
		}
	}
	if hasKeyword && extHot {
		r.Risk = "high"
	}
	if info.Size() > res.MaxFileSizeMB*1024*1024 {
		r.Tags = append(r.Tags, "大文件")
	}
	return r, true
}

// Cancel 取消进行中的扫描。
func (s *Scanner) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil && s.progress.Status == "running" {
		s.cancel()
	}
}

// Progress 返回当前进度快照。
func (s *Scanner) Progress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.progress
	if p.Status == "running" {
		p.ElapsedMS = time.Since(s.started).Milliseconds()
	}
	return p
}

// Results 返回结果副本；扫描完成前返回 nil。
func (s *Scanner) Results() []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Status != "done" && s.progress.Status != "cancelled" {
		return nil
	}
	out := make([]Result, len(s.results))
	copy(out, s.results)
	return out
}

// HasPath 判断路径是否在本次扫描结果中（server 的路径边界校验，spec 9.4）。
func (s *Scanner) HasPath(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.results {
		if r.Path == path {
			return true
		}
	}
	return false
}

// underAny 判断 path 是否位于 dirs 中任一目录树内（含目录本身）。
func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if path == d || strings.HasPrefix(path, d+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
