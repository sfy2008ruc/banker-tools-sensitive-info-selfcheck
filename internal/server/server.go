// Package server 实现本机 HTTP 服务：spec 第 7 章 API 与第 9 章安全设计。
package server

import (
	"crypto/subtle"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"selfcheck/internal/platform"
	"selfcheck/internal/quarantine"
	"selfcheck/internal/report"
	"selfcheck/internal/rules"
	"selfcheck/internal/scanner"
)

// Server 持有全部运行态（plan 3.5）。
type Server struct {
	Version  string
	Rules    *rules.Rules
	Resolved *rules.Resolved
	Scanner  *scanner.Scanner
	Quar     *quarantine.Quarantine
	Token    string
	ExeDir   string
	Static   fs.FS // web/dist 内嵌文件系统
	OnExit   func()

	mu       sync.Mutex
	lastBeat time.Time
}

// Handler 组装路由与中间件。
func (s *Server) Handler() http.Handler {
	s.mu.Lock()
	s.lastBeat = time.Now()
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(s.Static))) // 静态页免 token（token 经 URL 交给 JS）

	api := func(method string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !s.authOK(r) {
				http.Error(w, `{"error":"未授权"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			h(w, r)
		}
	}

	mux.HandleFunc("/api/scan", api(http.MethodPost, s.handleScan))
	mux.HandleFunc("/api/scan/progress", api(http.MethodGet, s.handleProgress))
	mux.HandleFunc("/api/scan/cancel", api(http.MethodPost, s.handleCancel))
	mux.HandleFunc("/api/results", api(http.MethodGet, s.handleResults))
	mux.HandleFunc("/api/quarantine", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api(http.MethodGet, s.handleQuarList)(w, r)
		case http.MethodPost:
			api(http.MethodPost, s.handleQuarMoveIn)(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/restore", api(http.MethodPost, s.handleRestore))
	mux.HandleFunc("/api/delete", api(http.MethodPost, s.handleDelete))
	mux.HandleFunc("/api/report", api(http.MethodGet, s.handleReport))
	mux.HandleFunc("/api/heartbeat", api(http.MethodPost, s.handleHeartbeat))
	mux.HandleFunc("/api/exit", api(http.MethodPost, s.handleExit))
	mux.HandleFunc("/api/open", api(http.MethodPost, s.handleOpen))
	mux.HandleFunc("/api/preview", api(http.MethodGet, s.handlePreview))
	return mux
}

// 打开文件/文件夹的系统调用做成可注入，便于单测替换（不真的弹出程序）。
var (
	openPathFn   = platform.OpenPath
	revealPathFn = platform.RevealPath
)

// authOK：X-Token 常量时间比较 + POST 同源校验（spec 9.2/9.3）。
func (s *Server) authOK(r *http.Request) bool {
	tok := r.Header.Get("X-Token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
		return false
	}
	if r.Method == http.MethodPost {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				return false
			}
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": msg})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	// V1.1：补充搜索词——仅本次扫描生效，与规则词合并，不落盘、不能删除既有词
	var body struct {
		ExtraKeywords []string `json:"extraKeywords"`
	}
	json.NewDecoder(r.Body).Decode(&body) // 空请求体合法，忽略解码错误

	resolved := s.Resolved
	if extraKw := sanitizeKeywords(body.ExtraKeywords, s.Resolved.Keywords); len(extraKw) > 0 {
		clone := *s.Resolved
		clone.Keywords = append(append([]string{}, s.Resolved.Keywords...), extraKw...)
		resolved = &clone
	}

	extra := []string{s.Quar.Root(), s.ExeDir}
	if !s.Scanner.Start(resolved, extra, platform.ExpandAll) {
		writeErr(w, http.StatusConflict, "扫描进行中")
		return
	}
	writeJSON(w, map[string]string{"status": "started"})
}

// sanitizeKeywords 清洗补充搜索词：去空白、去重、剔除与规则词重复项，限制条数与长度。
func sanitizeKeywords(in, existing []string) []string {
	seen := map[string]bool{}
	for _, k := range existing {
		seen[k] = true
	}
	var out []string
	for _, k := range in {
		k = strings.TrimSpace(k)
		if k == "" || len([]rune(k)) > 50 || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

// handleOpen 打开文件或所在文件夹；仅允许本次扫描结果内的路径（spec 9.4 同款边界）。
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Mode string `json:"mode"` // file | folder
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		writeErr(w, http.StatusBadRequest, "请求体应为 {path, mode}")
		return
	}
	if !s.Scanner.HasPath(body.Path) {
		writeErr(w, http.StatusBadRequest, "路径不在本次扫描结果中")
		return
	}
	var err error
	if body.Mode == "folder" {
		err = revealPathFn(body.Path)
	} else {
		err = openPathFn(body.Path)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "打开失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// handlePreview 返回图片缩略图；路径边界同上，响应禁止缓存（敏感内容）。
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || !s.Scanner.HasPath(path) {
		writeErr(w, http.StatusBadRequest, "路径不在本次扫描结果中")
		return
	}
	data, err := thumbnail(path, 480)
	if err != nil {
		writeErr(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func (s *Server) handleProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.Scanner.Progress())
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	s.Scanner.Cancel()
	writeJSON(w, map[string]string{"status": "cancelling"})
}

func (s *Server) handleResults(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"results":     s.Scanner.Results(),
		"ruleVersion": s.Resolved.Version,
		"ruleBuiltin": s.Resolved.BuiltinFallback,
	})
}

// handleQuarMoveIn：仅接受本次扫描结果内的路径（spec 9.4 防路径遍历）。
func (s *Server) handleQuarMoveIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, "请求体应为 {paths:[...]}")
		return
	}
	files := make([]quarantine.MoveInFile, 0, len(body.Paths))
	resultsByPath := map[string][]string{}
	for _, res := range s.Scanner.Results() {
		resultsByPath[res.Path] = res.MatchedRules
	}
	for _, p := range body.Paths {
		matched, ok := resultsByPath[p]
		if !ok {
			writeErr(w, http.StatusBadRequest, "路径不在本次扫描结果中: "+p)
			return
		}
		files = append(files, quarantine.MoveInFile{Path: p, MatchedRules: matched})
	}
	batch, results := s.Quar.MoveIn(files)
	writeJSON(w, map[string]any{"batch": batch, "results": results})
}

func (s *Server) handleQuarList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"entries": s.Quar.Pending(), "root": s.Quar.Root()})
}

func decodeIDs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "请求体应为 {ids:[...]}")
		return nil, false
	}
	return body.IDs, true
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"results": s.Quar.Restore(ids)})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"results": s.Quar.Delete(ids)})
}

func (s *Server) handleReport(w http.ResponseWriter, _ *http.Request) {
	p := s.Scanner.Progress()
	html, path, err := report.Generate(s.Quar, report.Stats{
		AppVersion:   s.Version,
		RuleVersion:  s.Resolved.Version,
		RuleBuiltin:  s.Resolved.BuiltinFallback,
		ScanRoots:    p.Roots,
		DirsScanned:  p.DirsScanned,
		Found:        p.Found,
		Skipped:      p.SkippedNoAccess,
		ScanElapsedS: float64(p.ElapsedMS) / 1000,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "报告生成失败: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"html": string(html), "savedTo": path})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.lastBeat = time.Now()
	s.mu.Unlock()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleExit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "bye"})
	if s.OnExit != nil {
		go s.OnExit()
	}
}

// StartWatchdog 每 interval 检查一次心跳，超过 timeout 无心跳则触发 OnExit（spec 4.1）。
// 扫描进行中不计超时（长扫描时页面可能被切到后台）。
func (s *Server) StartWatchdog(interval, timeout time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			if s.Scanner.Progress().Status == "running" {
				continue
			}
			s.mu.Lock()
			idle := time.Since(s.lastBeat)
			s.mu.Unlock()
			if idle > timeout {
				if s.OnExit != nil {
					s.OnExit()
				}
				return
			}
		}
	}()
}

// LoopbackOnly 拒绝非回环来源（双保险；监听本就绑定 127.0.0.1）。
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
