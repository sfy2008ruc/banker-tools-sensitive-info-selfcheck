// Package quarantine 实现"待删除暂存区"：移入、manifest 台账、还原、回收站删除（spec 6）。
package quarantine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"selfcheck/internal/platform"
)

// DirName 暂存区目录名，位于用户主目录下（spec 6.1）。
const DirName = "待删除文件区"

// Entry 对应 manifest.json 的一条记录（spec 6.2）。
type Entry struct {
	ID           string    `json:"id"`
	Batch        string    `json:"batch"`
	StoredName   string    `json:"storedName"`
	OriginalPath string    `json:"originalPath"`
	SizeBytes    int64     `json:"sizeBytes"`
	ModifiedAt   time.Time `json:"modifiedAt"`
	MatchedRules []string  `json:"matchedRules"`
	MovedAt      time.Time `json:"movedAt"`
	Status       string    `json:"status"` // pending | restored | deleted
	RestoredTo   string    `json:"restoredTo,omitempty"`
}

type manifest struct {
	Entries []Entry `json:"entries"`
}

// OpResult 是单文件操作结果：单文件失败不影响其余（spec F4）。
type OpResult struct {
	Path  string `json:"path"` // 移入时=原路径；还原/删除时=条目 id
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Quarantine 持有暂存区根目录与 manifest；所有变更立即原子落盘。
type Quarantine struct {
	mu   sync.Mutex
	root string
	m    manifest
}

// Open 打开（或初始化）home 下的暂存区并加载 manifest。
func Open(home string) (*Quarantine, error) {
	root := filepath.Join(home, DirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	q := &Quarantine{root: root}
	data, err := os.ReadFile(q.manifestPath())
	if err == nil {
		// manifest 损坏时不静默清空台账，避免暂存文件失联
		if jerr := json.Unmarshal(data, &q.m); jerr != nil {
			return nil, fmt.Errorf("manifest.json 已损坏，请人工检查 %s: %w", q.manifestPath(), jerr)
		}
	}
	return q, nil
}

func (q *Quarantine) Root() string         { return q.root }
func (q *Quarantine) manifestPath() string { return filepath.Join(q.root, "manifest.json") }

// save 原子落盘：临时文件 + rename（spec 6.2，强杀进程后状态一致）。
func (q *Quarantine) save() error {
	data, err := json.MarshalIndent(&q.m, "", "  ")
	if err != nil {
		return err
	}
	tmp := q.manifestPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.manifestPath())
}

// MoveInFile 描述一个待移入文件及其命中原因。
type MoveInFile struct {
	Path         string
	MatchedRules []string
}

// MoveIn 将文件移入新批次；返回批次号与逐条结果。
func (q *Quarantine) MoveIn(files []MoveInFile) (string, []OpResult) {
	q.mu.Lock()
	defer q.mu.Unlock()

	batch := time.Now().Format("20060102-150405")
	batchDir := filepath.Join(q.root, batch)
	results := make([]OpResult, 0, len(files))
	// 同一秒内多次移入会命中同名批次：序号接续既有条目，保证 ID 全局唯一
	seq := 0
	for _, e := range q.m.Entries {
		if e.Batch == batch {
			var n int
			fmt.Sscanf(e.StoredName, "%04d_", &n)
			if n > seq {
				seq = n
			}
		}
	}

	for _, f := range files {
		res := OpResult{Path: f.Path}
		info, err := os.Stat(f.Path)
		switch {
		case err != nil:
			res.Error = "文件不存在或不可访问"
		case info.IsDir():
			res.Error = "不支持移动目录"
		default:
			if err := os.MkdirAll(batchDir, 0o755); err != nil {
				res.Error = "无法创建批次目录: " + err.Error()
				break
			}
			seq++
			storedName := fmt.Sprintf("%04d_%s", seq, filepath.Base(f.Path))
			if err := platform.MovePath(f.Path, filepath.Join(batchDir, storedName)); err != nil {
				res.Error = "移动失败（源文件已保留）: " + err.Error()
				seq--
				break
			}
			q.m.Entries = append(q.m.Entries, Entry{
				ID:           fmt.Sprintf("%s-%04d", batch, seq),
				Batch:        batch,
				StoredName:   storedName,
				OriginalPath: f.Path,
				SizeBytes:    info.Size(),
				ModifiedAt:   info.ModTime(),
				MatchedRules: f.MatchedRules,
				MovedAt:      time.Now(),
				Status:       "pending",
			})
			res.OK = true
		}
		results = append(results, res)
	}
	if err := q.save(); err != nil {
		// 落盘失败是严重问题：文件已移动但台账未记录，逐条标注
		for i := range results {
			if results[i].OK {
				results[i].Error = "警告：manifest 落盘失败，请勿关闭程序: " + err.Error()
			}
		}
	}
	return batch, results
}

// Pending 返回待确认条目。
func (q *Quarantine) Pending() []Entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []Entry
	for _, e := range q.m.Entries {
		if e.Status == "pending" {
			out = append(out, e)
		}
	}
	return out
}

// All 返回全部条目（报告数据源）。
func (q *Quarantine) All() []Entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Entry, len(q.m.Entries))
	copy(out, q.m.Entries)
	return out
}

func (q *Quarantine) storedPath(e *Entry) string {
	return filepath.Join(q.root, e.Batch, e.StoredName)
}

func (q *Quarantine) find(id string) *Entry {
	for i := range q.m.Entries {
		if q.m.Entries[i].ID == id {
			return &q.m.Entries[i]
		}
	}
	return nil
}

// Restore 还原条目至原路径；冲突时改名 `原名(还原-150405).后缀`，目录不存在则重建（spec 6.3）。
func (q *Quarantine) Restore(ids []string) []OpResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	results := make([]OpResult, 0, len(ids))
	for _, id := range ids {
		res := OpResult{Path: id}
		e := q.find(id)
		switch {
		case e == nil:
			res.Error = "条目不存在"
		case e.Status != "pending":
			res.Error = "条目已处理（" + e.Status + "）"
		default:
			dst := e.OriginalPath
			if _, err := os.Lstat(dst); err == nil {
				ext := filepath.Ext(dst)
				dst = dst[:len(dst)-len(ext)] +
					fmt.Sprintf("(还原-%s)", time.Now().Format("150405")) + ext
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				res.Error = "无法重建原目录: " + err.Error()
				break
			}
			if err := platform.MovePath(q.storedPath(e), dst); err != nil {
				res.Error = "还原失败: " + err.Error()
				break
			}
			e.Status = "restored"
			e.RestoredTo = dst
			res.OK = true
		}
		results = append(results, res)
	}
	q.save()
	q.cleanupEmptyBatches()
	return results
}

// Delete 将条目移入系统回收站（spec 6.3：不物理删除）。
func (q *Quarantine) Delete(ids []string) []OpResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	results := make([]OpResult, 0, len(ids))
	for _, id := range ids {
		res := OpResult{Path: id}
		e := q.find(id)
		switch {
		case e == nil:
			res.Error = "条目不存在"
		case e.Status != "pending":
			res.Error = "条目已处理（" + e.Status + "）"
		default:
			if err := platform.MoveToTrash(q.storedPath(e)); err != nil {
				res.Error = "移入回收站失败: " + err.Error()
				break
			}
			e.Status = "deleted"
			res.OK = true
		}
		results = append(results, res)
	}
	q.save()
	q.cleanupEmptyBatches()
	return results
}

// cleanupEmptyBatches 删除已清空的批次目录（调用方需持锁）。
func (q *Quarantine) cleanupEmptyBatches() {
	entries, err := os.ReadDir(q.root)
	if err != nil {
		return
	}
	for _, d := range entries {
		if !d.IsDir() || d.Name() == "reports" {
			continue
		}
		dir := filepath.Join(q.root, d.Name())
		if sub, err := os.ReadDir(dir); err == nil && len(sub) == 0 {
			os.Remove(dir)
		}
	}
}
