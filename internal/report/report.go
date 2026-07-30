// Package report 生成 HTML 自查报告并落盘 reports/（spec 3.1 F7、plan 3.6）。
package report

import (
	"bytes"
	"html/template"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"selfcheck/internal/quarantine"
)

// Stats 是本次自查的汇总数据，由 server 汇集后传入。
type Stats struct {
	AppVersion   string
	RuleVersion  string
	RuleBuiltin  bool
	ScanRoots    []string
	DirsScanned  int64
	Found        int64
	Skipped      int64
	ScanElapsedS float64
}

const tpl = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<title>电脑敏感文件自查报告</title>
<style>
body{font-family:"Microsoft YaHei",sans-serif;max-width:900px;margin:24px auto;padding:0 16px;color:#222}
h1{font-size:22px;border-bottom:2px solid #2b6cb0;padding-bottom:8px}
table{border-collapse:collapse;width:100%;margin:12px 0;font-size:13px}
th,td{border:1px solid #ccc;padding:6px 8px;text-align:left;word-break:break-all}
th{background:#f0f4f8}
.meta td:first-child{width:160px;background:#f7f7f7}
.tag{display:inline-block;background:#edf2f7;border-radius:3px;padding:1px 6px;margin-right:4px;font-size:12px}
.del{color:#c53030}.res{color:#2f855a}
footer{margin-top:24px;font-size:12px;color:#888}
</style></head><body>
<h1>电脑敏感文件自查报告</h1>
<table class="meta">
<tr><td>自查时间</td><td>{{.Now}}</td></tr>
<tr><td>操作人 / 主机</td><td>{{.User}} @ {{.Host}}</td></tr>
<tr><td>工具版本 / 规则版本</td><td>{{.S.AppVersion}} / {{.S.RuleVersion}}{{if .S.RuleBuiltin}}（内置规则）{{end}}</td></tr>
<tr><td>扫描范围</td><td>{{range .S.ScanRoots}}<span class="tag">{{.}}</span>{{end}}</td></tr>
<tr><td>扫描统计</td><td>遍历目录 {{.S.DirsScanned}} 个，发现可疑文件 {{.S.Found}} 个，无权限跳过 {{.S.Skipped}} 处，耗时 {{printf "%.1f" .S.ScanElapsedS}} 秒</td></tr>
<tr><td>处理统计</td><td>累计暂存 {{.Total}} 个：<span class="del">已删除 {{.Deleted}}</span>、<span class="res">已还原 {{.Restored}}</span>、待处理 {{.Pending}}</td></tr>
</table>
{{if .Entries}}
<h2 style="font-size:16px">处理明细</h2>
<table>
<tr><th>#</th><th>原路径</th><th>大小</th><th>命中规则</th><th>移入时间</th><th>处理结果</th></tr>
{{range $i, $e := .Entries}}
<tr><td>{{inc $i}}</td><td>{{$e.OriginalPath}}</td><td>{{kb $e.SizeBytes}}</td>
<td>{{range $e.MatchedRules}}<span class="tag">{{.}}</span>{{end}}</td>
<td>{{$e.MovedAt.Format "2006-01-02 15:04"}}</td>
<td>{{if eq $e.Status "deleted"}}<span class="del">已删除</span>{{else if eq $e.Status "restored"}}<span class="res">已还原</span>{{else}}待处理{{end}}</td></tr>
{{end}}
</table>
{{end}}
<footer>本报告由《客户经理电脑敏感文件自查工具》自动生成，仅存于本机，未上传任何数据。</footer>
</body></html>`

var reportTpl = template.Must(template.New("r").Funcs(template.FuncMap{
	"inc": func(i int) int { return i + 1 },
	"kb":  func(b int64) string { return template.HTMLEscapeString(humanSize(b)) },
}).Parse(tpl))

func humanSize(b int64) string {
	switch {
	case b >= 1<<20:
		return itoa(b/(1<<20)) + " MB"
	case b >= 1<<10:
		return itoa(b/(1<<10)) + " KB"
	default:
		return itoa(b) + " B"
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Generate 渲染报告并落盘 <暂存区>/reports/，返回 HTML 内容与落盘路径。
func Generate(q *quarantine.Quarantine, s Stats) ([]byte, string, error) {
	entries := q.All()
	var deleted, restored, pending int
	for _, e := range entries {
		switch e.Status {
		case "deleted":
			deleted++
		case "restored":
			restored++
		default:
			pending++
		}
	}
	userName := "未知"
	if u, err := user.Current(); err == nil {
		userName = u.Username
	}
	host, _ := os.Hostname()

	data := struct {
		Now, User, Host                   string
		S                                 Stats
		Entries                           []quarantine.Entry
		Total, Deleted, Restored, Pending int
	}{
		Now: time.Now().Format("2006-01-02 15:04:05"), User: userName, Host: host,
		S: s, Entries: entries,
		Total: len(entries), Deleted: deleted, Restored: restored, Pending: pending,
	}

	var buf bytes.Buffer
	if err := reportTpl.Execute(&buf, data); err != nil {
		return nil, "", err
	}
	dir := filepath.Join(q.Root(), "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "自查报告-"+time.Now().Format("20060102-150405")+".html")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), path, nil
}
