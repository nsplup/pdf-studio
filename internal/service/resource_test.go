package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBookmarkClampOnSave 覆盖「书签页码超出当前页数 → 保存/另存失败」的回归：
// 1 页文档 + 指向第 2 页的书签，另存时后端应钳制页码而非报 invalid bookmark。
func TestBookmarkClampOnSave(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 1)

	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	store := NewDocStore(ws)
	ds := NewDocumentService(NewEventBus(), store)

	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	out := filepath.Join(dir, "out.pdf")
	page2 := 2
	info, err = ds.SaveAs(info.ID, out, &SaveOptions{
		Outline: &[]OutlineNode{{Title: "书签1", Page: page2}},
	})
	if err != nil {
		t.Fatalf("SaveAs with out-of-range bookmark page: %v", err)
	}
	_ = info
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
}

// TestPageResourcesAndSeqAssembly 内容寻址资源 + 保存装配：
// PageResources 返回 uuid 序列；提交重排后的 pageSeq 保存后页面顺序应随之改变。
func TestPageResourcesAndSeqAssembly(t *testing.T) {
	dir := t.TempDir()
	// 两页尺寸不同的文档，便于区分页序
	src := makePDF(t, dir, 2)

	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	store := NewDocStore(ws)
	ds := NewDocumentService(NewEventBus(), store)

	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	res, err := ds.PageResources(info.ID)
	if err != nil {
		t.Fatalf("PageResources: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("res count = %d, want 2", len(res))
	}
	for _, u := range res {
		if len(u) != 40 {
			t.Fatalf("uuid %q not sha1 hex", u)
		}
		// 基图已生成
		if _, err := os.Stat(filepath.Join(ws.ResourceDir(u), "140.png")); err != nil {
			t.Fatalf("base thumb missing for %s: %v", u, err)
		}
	}

	// 提交重排后的序列（[p2, p1]）
	rev := []string{res[1], res[0]}
	out := filepath.Join(dir, "reordered.pdf")
	if _, err := ds.SaveAs(info.ID, out, &SaveOptions{PageSeq: &rev}); err != nil {
		t.Fatalf("SaveAs with pageSeq: %v", err)
	}

	// 校验：新工作版本的页面 1 应与原页面 2 内容一致（资源 uuid 相同）
	info2, err := ds.Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	res2, err := ds.PageResources(info2.ID)
	if err != nil {
		t.Fatalf("PageResources after save: %v", err)
	}
	if len(res2) != 2 || res2[0] != res[1] || res2[1] != res[0] {
		t.Fatalf("page order not preserved: got %v want [%s %s]", res2, res[1], res[0])
	}
}

// TestBlankPageAssembly 空白页标记在保存时生成对应尺寸页面。
func TestBlankPageAssembly(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 1)

	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	store := NewDocStore(ws)
	ds := NewDocumentService(NewEventBus(), store)

	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res, err := ds.PageResources(info.ID)
	if err != nil {
		t.Fatalf("PageResources: %v", err)
	}

	// 原 1 页后追加一张 A4 空白页
	seq := append(append([]string{}, res[0]), "blank-595x842")
	out := filepath.Join(dir, "with-blank.pdf")
	if _, err := ds.SaveAs(info.ID, out, &SaveOptions{PageSeq: &seq}); err != nil {
		t.Fatalf("SaveAs with blank marker: %v", err)
	}
	info2, err := ds.Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if info2.PageCount != 2 {
		t.Fatalf("pageCount = %d, want 2", info2.PageCount)
	}
	// 空白页尺寸应为 595x842（±1pt 容差）
	dim := info2.Pages[1]
	if dim.Width < 594 || dim.Width > 596 || dim.Height < 841 || dim.Height > 843 {
		t.Fatalf("blank page dim = %vx%v, want ~595x842", dim.Width, dim.Height)
	}
	if strings.HasPrefix(seq[1], "blank-") != true {
		t.Fatalf("marker mutated")
	}
}

// TestBookmarkClampAfterPageSeq 页面序列改变页数后，书签页码应按装配后页数钳制：
// 2 页文档 → pageSeq 仅保留第 2 页（合并后 1 页）→ 书签指向第 2 页应钳到第 1 页而非报错。
func TestBookmarkClampAfterPageSeq(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 2)

	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	store := NewDocStore(ws)
	ds := NewDocumentService(NewEventBus(), store)

	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res, err := ds.PageResources(info.ID)
	if err != nil {
		t.Fatalf("PageResources: %v", err)
	}
	seq := []string{res[1]} // 删除第 1 页
	out := filepath.Join(dir, "clamped.pdf")
	page2 := 2
	if _, err := ds.SaveAs(info.ID, out, &SaveOptions{
		PageSeq: &seq,
		Outline: &[]OutlineNode{{Title: "指向原第2页", Page: page2}},
	}); err != nil {
		t.Fatalf("SaveAs with clamped bookmark: %v", err)
	}
	info2, err := ds.Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if info2.PageCount != 1 {
		t.Fatalf("pageCount = %d, want 1", info2.PageCount)
	}
}
