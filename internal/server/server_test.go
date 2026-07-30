package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"selfcheck/internal/quarantine"
	"selfcheck/internal/rules"
	"selfcheck/internal/scanner"
)

func newTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	home := t.TempDir()
	desktop := filepath.Join(home, "桌面")
	os.MkdirAll(desktop, 0o755)
	target := filepath.Join(desktop, "张三身份证.jpg")
	os.WriteFile(target, make([]byte, 40*1024), 0o644)

	q, err := quarantine.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Version: "test",
		Resolved: &rules.Resolved{
			Version:        "test-rules",
			Keywords:       []string{"身份证"},
			Extensions:     []string{".jpg"},
			HotDirs:        []string{desktop},
			ScanRoots:      []string{home},
			MinImageSizeKB: 30, MaxFileSizeMB: 500,
		},
		Scanner: scanner.New(),
		Quar:    q,
		Token:   "secret-token",
		ExeDir:  t.TempDir(),
		Static:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")}},
	}
	return s, home, target
}

func call(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("X-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func waitScanDone(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.Scanner.Progress().Status == "running" {
		if time.Now().After(deadline) {
			t.Fatal("扫描超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAuth(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := s.Handler()
	if w := call(t, h, "GET", "/api/results", "", nil); w.Code != 401 {
		t.Fatalf("无 token 应 401，got %d", w.Code)
	}
	if w := call(t, h, "GET", "/api/results", "wrong", nil); w.Code != 401 {
		t.Fatalf("错 token 应 401，got %d", w.Code)
	}
	if w := call(t, h, "GET", "/api/results", "secret-token", nil); w.Code != 200 {
		t.Fatalf("正确 token 应 200，got %d", w.Code)
	}
	// 静态页免 token
	if w := call(t, h, "GET", "/", "", nil); w.Code != 200 {
		t.Fatalf("静态页应 200，got %d", w.Code)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := s.Handler()
	req := httptest.NewRequest("POST", "/api/scan", nil)
	req.Header.Set("X-Token", "secret-token")
	req.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("跨源 POST 应被拒绝，got %d", w.Code)
	}
}

func TestScanConflict409(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := s.Handler()
	if w := call(t, h, "POST", "/api/scan", "secret-token", nil); w.Code != 200 {
		t.Fatalf("首次扫描应 200，got %d: %s", w.Code, w.Body)
	}
	// 立即再扫：若首次尚未完成应 409；小样本可能瞬间完成，两种结果都接受，但完成后再扫必须 200
	waitScanDone(t, s)
	if w := call(t, h, "POST", "/api/scan", "secret-token", nil); w.Code != 200 {
		t.Fatalf("完成后再扫应 200，got %d", w.Code)
	}
	if w := call(t, h, "POST", "/api/scan", "secret-token", nil); w.Code != 409 {
		t.Fatalf("进行中再扫应 409，got %d", w.Code)
	}
	waitScanDone(t, s)
}

func TestFullFlow(t *testing.T) {
	s, _, target := newTestServer(t)
	h := s.Handler()

	// scan → done
	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)

	// results 应包含目标文件
	w := call(t, h, "GET", "/api/results", "secret-token", nil)
	var res struct {
		Results []scanner.Result `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Results) != 1 || res.Results[0].Path != target || res.Results[0].Risk != "high" {
		t.Fatalf("扫描结果错误: %+v", res.Results)
	}

	// 伪造路径 → 400
	if w := call(t, h, "POST", "/api/quarantine", "secret-token",
		map[string]any{"paths": []string{"/etc/passwd"}}); w.Code != 400 {
		t.Fatalf("伪造路径应 400，got %d", w.Code)
	}

	// 合法移入
	w = call(t, h, "POST", "/api/quarantine", "secret-token", map[string]any{"paths": []string{target}})
	if w.Code != 200 {
		t.Fatalf("移入应 200，got %d: %s", w.Code, w.Body)
	}
	var quarList struct {
		Entries []quarantine.Entry `json:"entries"`
	}
	w = call(t, h, "GET", "/api/quarantine", "secret-token", nil)
	json.Unmarshal(w.Body.Bytes(), &quarList)
	if len(quarList.Entries) != 1 {
		t.Fatalf("暂存区应有 1 条，got %+v", quarList.Entries)
	}
	id := quarList.Entries[0].ID

	// 还原 → 再移入 → 删除
	if w := call(t, h, "POST", "/api/restore", "secret-token", map[string]any{"ids": []string{id}}); w.Code != 200 {
		t.Fatalf("还原应 200：%s", w.Body)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("还原后原路径应恢复")
	}
	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)
	call(t, h, "POST", "/api/quarantine", "secret-token", map[string]any{"paths": []string{target}})
	w = call(t, h, "GET", "/api/quarantine", "secret-token", nil)
	quarList.Entries = nil
	json.Unmarshal(w.Body.Bytes(), &quarList)
	w = call(t, h, "POST", "/api/delete", "secret-token", map[string]any{"ids": []string{quarList.Entries[0].ID}})
	var delRes struct {
		Results []quarantine.OpResult `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &delRes)
	if !delRes.Results[0].OK {
		t.Fatalf("删除失败: %+v", delRes.Results)
	}

	// 报告
	w = call(t, h, "GET", "/api/report", "secret-token", nil)
	var rep map[string]string
	json.Unmarshal(w.Body.Bytes(), &rep)
	if w.Code != 200 || rep["savedTo"] == "" {
		t.Fatalf("报告生成失败: %d %s", w.Code, w.Body)
	}
}

func TestExitAndHeartbeat(t *testing.T) {
	s, _, _ := newTestServer(t)
	exited := make(chan struct{})
	s.OnExit = func() { close(exited) }
	h := s.Handler()

	if w := call(t, h, "POST", "/api/heartbeat", "secret-token", nil); w.Code != 200 {
		t.Fatalf("心跳应 200，got %d", w.Code)
	}
	if w := call(t, h, "POST", "/api/exit", "secret-token", nil); w.Code != 200 {
		t.Fatalf("退出应 200，got %d", w.Code)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("exit 应触发 OnExit")
	}
}

func TestLoopbackOnly(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	h := LoopbackOnly(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.5:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("非回环来源应 403，got %d", w.Code)
	}
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("回环来源应放行，got %d", w.Code)
	}
}

func TestScanExtraKeywords(t *testing.T) {
	s, home, _ := newTestServer(t)
	// 非热点目录、后缀不在规则表 → 仅可能被"补充搜索词"命中
	misc := filepath.Join(home, "杂项")
	os.MkdirAll(misc, 0o755)
	secret := filepath.Join(misc, "机密名单.txt")
	os.WriteFile(secret, []byte("x"), 0o644)
	h := s.Handler()

	// 不带补充词：不命中
	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)
	if s.Scanner.HasPath(secret) {
		t.Fatal("未加补充词时不应命中")
	}
	// 带补充词"机密"：命中；且不落盘（Resolved 不被污染）
	before := len(s.Resolved.Keywords)
	call(t, h, "POST", "/api/scan", "secret-token",
		map[string]any{"extraKeywords": []string{"机密", "  ", "身份证"}}) // 空白与既有词应被剔除
	waitScanDone(t, s)
	if !s.Scanner.HasPath(secret) {
		t.Fatal("补充词应命中 机密名单.txt")
	}
	if len(s.Resolved.Keywords) != before {
		t.Fatal("补充词不应写回规则")
	}
	// 再扫一次不带补充词：仅本次生效
	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)
	if s.Scanner.HasPath(secret) {
		t.Fatal("补充词只应对单次扫描生效")
	}
}

func TestOpenEndpoint(t *testing.T) {
	s, _, target := newTestServer(t)
	var opened, revealed string
	openPathFn = func(p string) error { opened = p; return nil }
	revealPathFn = func(p string) error { revealed = p; return nil }
	defer func() { openPathFn = nil; revealPathFn = nil }()
	h := s.Handler()

	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)

	if w := call(t, h, "POST", "/api/open", "secret-token",
		map[string]any{"path": "/etc/passwd", "mode": "file"}); w.Code != 400 {
		t.Fatalf("伪造路径应 400，got %d", w.Code)
	}
	if w := call(t, h, "POST", "/api/open", "secret-token",
		map[string]any{"path": target, "mode": "file"}); w.Code != 200 || opened != target {
		t.Fatalf("打开文件失败: %d opened=%s", w.Code, opened)
	}
	if w := call(t, h, "POST", "/api/open", "secret-token",
		map[string]any{"path": target, "mode": "folder"}); w.Code != 200 || revealed != target {
		t.Fatalf("打开文件夹失败: %d revealed=%s", w.Code, revealed)
	}
}

func TestPreviewEndpoint(t *testing.T) {
	s, home, _ := newTestServer(t)
	s.Resolved.MinImageSizeKB = 0 // 测试用小 PNG，关掉小图片过滤
	// 桌面放一张真实 PNG（60x40），文件名含敏感词保证命中
	img := image.NewRGBA(image.Rect(0, 0, 60, 40))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	pngPath := filepath.Join(home, "桌面", "身份证样片.png")
	os.WriteFile(pngPath, buf.Bytes(), 0o644)
	h := s.Handler()

	call(t, h, "POST", "/api/scan", "secret-token", nil)
	waitScanDone(t, s)

	if w := call(t, h, "GET", "/api/preview?path="+url.QueryEscape("/etc/passwd"), "secret-token", nil); w.Code != 400 {
		t.Fatalf("伪造路径应 400，got %d", w.Code)
	}
	w := call(t, h, "GET", "/api/preview?path="+url.QueryEscape(pngPath), "secret-token", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("预览应返回 jpeg：%d %s %s", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	if _, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatalf("返回内容应为合法 JPEG: %v", err)
	}
}

func TestConfigEndpoint(t *testing.T) {
	s, _, _ := newTestServer(t)
	w := call(t, s.Handler(), "GET", "/api/config", "secret-token", nil)
	if w.Code != 200 {
		t.Fatalf("config 应 200，got %d", w.Code)
	}
	var c struct {
		RuleVersion string   `json:"ruleVersion"`
		Keywords    []string `json:"keywords"`
		Extensions  []string `json:"extensions"`
	}
	json.Unmarshal(w.Body.Bytes(), &c)
	if c.RuleVersion != "test-rules" || len(c.Keywords) != 1 || c.Keywords[0] != "身份证" {
		t.Fatalf("config 内容错误: %+v", c)
	}
	if len(c.Extensions) != 1 || c.Extensions[0] != ".jpg" {
		t.Fatalf("后缀应下发: %+v", c.Extensions)
	}
	// 未授权
	if w := call(t, s.Handler(), "GET", "/api/config", "", nil); w.Code != 401 {
		t.Fatalf("无 token 应 401，got %d", w.Code)
	}
}
