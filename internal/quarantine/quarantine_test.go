package quarantine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (home string, q *Quarantine, srcFile string) {
	t.Helper()
	home = t.TempDir()
	src := filepath.Join(home, "桌面")
	os.MkdirAll(src, 0o755)
	srcFile = filepath.Join(src, "身份证.jpg")
	os.WriteFile(srcFile, []byte("photo-data"), 0o644)
	var err error
	q, err = Open(home)
	if err != nil {
		t.Fatal(err)
	}
	return home, q, srcFile
}

func TestMoveInAndManifest(t *testing.T) {
	_, q, srcFile := setup(t)
	batch, res := q.MoveIn([]MoveInFile{{Path: srcFile, MatchedRules: []string{"keyword:身份证"}}})
	if !res[0].OK {
		t.Fatalf("移入失败: %+v", res[0])
	}
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatal("移入后源文件应消失")
	}
	pending := q.Pending()
	if len(pending) != 1 || pending[0].Status != "pending" || pending[0].Batch != batch {
		t.Fatalf("manifest 状态错误: %+v", pending)
	}
	if !strings.HasPrefix(pending[0].StoredName, "0001_") {
		t.Fatalf("存储名应带序号前缀: %s", pending[0].StoredName)
	}
	// 暂存文件物理存在
	if _, err := os.Stat(filepath.Join(q.Root(), batch, pending[0].StoredName)); err != nil {
		t.Fatal("暂存区应存在该文件")
	}
}

func TestMoveInMissingFileDoesNotBlockOthers(t *testing.T) {
	_, q, srcFile := setup(t)
	_, res := q.MoveIn([]MoveInFile{
		{Path: filepath.Join(t.TempDir(), "不存在.pdf")},
		{Path: srcFile},
	})
	if res[0].OK || res[0].Error == "" {
		t.Fatalf("不存在的文件应失败: %+v", res[0])
	}
	if !res[1].OK {
		t.Fatalf("其余文件应继续成功: %+v", res[1])
	}
}

func TestRestore(t *testing.T) {
	_, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	id := q.Pending()[0].ID

	res := q.Restore([]string{id})
	if !res[0].OK {
		t.Fatalf("还原失败: %+v", res[0])
	}
	if data, err := os.ReadFile(srcFile); err != nil || string(data) != "photo-data" {
		t.Fatal("应还原到原路径且内容一致")
	}
	if len(q.Pending()) != 0 {
		t.Fatal("还原后不应有 pending 条目")
	}
}

func TestRestoreConflictRenames(t *testing.T) {
	_, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	id := q.Pending()[0].ID
	// 原路径被新文件占用
	os.WriteFile(srcFile, []byte("newer"), 0o644)

	res := q.Restore([]string{id})
	if !res[0].OK {
		t.Fatalf("冲突还原失败: %+v", res[0])
	}
	restoredTo := q.All()[0].RestoredTo
	if restoredTo == srcFile || !strings.Contains(restoredTo, "(还原-") {
		t.Fatalf("冲突时应改名还原: %s", restoredTo)
	}
	if data, _ := os.ReadFile(restoredTo); string(data) != "photo-data" {
		t.Fatal("改名还原的内容应为原文件")
	}
	if data, _ := os.ReadFile(srcFile); string(data) != "newer" {
		t.Fatal("占用原路径的新文件不应被覆盖")
	}
}

func TestRestoreRebuildsMissingDir(t *testing.T) {
	home, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	id := q.Pending()[0].ID
	os.RemoveAll(filepath.Join(home, "桌面"))

	res := q.Restore([]string{id})
	if !res[0].OK {
		t.Fatalf("目录被删后还原应重建目录: %+v", res[0])
	}
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatal("还原后文件应在重建的原目录中")
	}
}

func TestReloadKeepsState(t *testing.T) {
	home, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	id := q.Pending()[0].ID

	// 模拟强杀重启：重新 Open
	q2, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	p := q2.Pending()
	if len(p) != 1 || p[0].ID != id {
		t.Fatalf("重启后 manifest 状态应一致: %+v", p)
	}
}

func TestCorruptManifestRefusesToOpen(t *testing.T) {
	home, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	os.WriteFile(filepath.Join(q.Root(), "manifest.json"), []byte("{broken"), 0o644)
	if _, err := Open(home); err == nil {
		t.Fatal("损坏的 manifest 应拒绝打开而非清空台账")
	}
}

func TestDeleteMovesToTrash(t *testing.T) {
	_, q, srcFile := setup(t)
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	e := q.Pending()[0]

	res := q.Delete([]string{e.ID})
	if !res[0].OK {
		t.Fatalf("删除失败: %+v", res[0])
	}
	if _, err := os.Stat(filepath.Join(q.Root(), e.Batch, e.StoredName)); !os.IsNotExist(err) {
		t.Fatal("删除后暂存区文件应消失（已入回收站）")
	}
	if len(q.Pending()) != 0 {
		t.Fatal("删除后不应有 pending 条目")
	}
	if q.All()[0].Status != "deleted" {
		t.Fatal("状态应为 deleted")
	}
	// 二次删除同一条目应报已处理
	res2 := q.Delete([]string{e.ID})
	if res2[0].OK {
		t.Fatal("重复删除应失败")
	}
}

func TestMoveInSameSecondUniqueIDs(t *testing.T) {
	home, q, srcFile := setup(t)
	src2 := filepath.Join(home, "桌面", "合同.pdf")
	os.WriteFile(src2, []byte("pdf"), 0o644)

	// 两次移入在同一秒内完成，ID 仍须全局唯一
	q.MoveIn([]MoveInFile{{Path: srcFile}})
	q.MoveIn([]MoveInFile{{Path: src2}})
	all := q.All()
	if len(all) != 2 {
		t.Fatalf("应有 2 条: %+v", all)
	}
	if all[0].ID == all[1].ID {
		t.Fatalf("同秒批次 ID 冲突: %s", all[0].ID)
	}
}
