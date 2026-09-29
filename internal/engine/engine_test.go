package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"bytes"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// createTestPDF 生成指定页数的测试 PDF，并附带逐页书签。
func createTestPDF(t *testing.T, dir string, pages int) string {
	return createTestPDFBM(t, dir, pages, true)
}

func createTestPDFBM(t *testing.T, dir string, pages int, withBM bool) string {
	t.Helper()
	// 按 pdfcpu 官方测试方式构造带样例页的单页 PDF
	mediaBox := types.RectForFormat("A4")
	page := model.Page{MediaBox: mediaBox, Fm: model.FontMap{}, Buf: new(bytes.Buffer)}
	pdfcpu.CreateTestPageContent(page)
	xRefTable, err := pdfcpu.CreateDemoXRef()
	if err != nil {
		t.Fatalf("CreateDemoXRef: %v", err)
	}
	rootDict, err := xRefTable.Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if err := pdfcpu.AddPageTreeWithSamplePage(xRefTable, rootDict, page); err != nil {
		t.Fatalf("AddPageTreeWithSamplePage: %v", err)
	}
	base := filepath.Join(dir, fmt.Sprintf("base-%d.pdf", pages))
	if err := api.CreatePDFFile(xRefTable, base, Config()); err != nil {
		t.Fatalf("CreatePDFFile: %v", err)
	}
	// 通过插入空白页扩展页数（1 → pages）
	if pages > 1 {
		grown := filepath.Join(dir, fmt.Sprintf("grown-%d.pdf", pages))
		if err := InsertBlankPages(base, grown, 1, pages-1, true, nil); err != nil {
			t.Fatalf("InsertBlankPages: %v", err)
		}
		base = grown
	}
	if n, _ := PageCount(base); n != pages {
		t.Fatalf("test pdf pages = %d, want %d", n, pages)
	}
	// 附加逐页书签
	bookmarks := []pdfcpu.Bookmark{}
	for i := 0; i < pages; i++ {
		bookmarks = append(bookmarks, pdfcpu.Bookmark{
			Title:    fmt.Sprintf("第 %d 页", i+1),
			PageFrom: i + 1,
		})
	}
	out := base
	if withBM {
		out = filepath.Join(dir, fmt.Sprintf("test-%d.pdf", pages))
		if err := api.AddBookmarksFile(base, out, bookmarks, true, Config()); err != nil {
			t.Fatalf("AddBookmarksFile: %v", err)
		}
	}
	return out
}

func TestPageCountAndDims(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 5)
	n, err := PageCount(p)
	if err != nil || n != 5 {
		t.Fatalf("PageCount = %d, %v; want 5", n, err)
	}
	dims, err := PageDims(p)
	if err != nil || len(dims) != 5 {
		t.Fatalf("PageDims = %d, %v", len(dims), err)
	}
}

func TestRemovePages(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 6)
	out := filepath.Join(dir, "removed.pdf")
	if err := RemovePages(p, out, "2,4"); err != nil {
		t.Fatalf("RemovePages: %v", err)
	}
	n, _ := PageCount(out)
	if n != 4 {
		t.Fatalf("after remove pages = %d, want 4", n)
	}
}

func TestInsertBlankPages(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 3)
	out := filepath.Join(dir, "blank.pdf")
	dim, _ := BlankPageDimFor(p, 2)
	if err := InsertBlankPages(p, out, 2, 2, true, dim); err != nil {
		t.Fatalf("InsertBlankPages: %v", err)
	}
	n, _ := PageCount(out)
	if n != 5 {
		t.Fatalf("after insert blank = %d, want 5", n)
	}
}

func TestInsertFromPDF(t *testing.T) {
	dir := t.TempDir()
	base := createTestPDFBM(t, dir, 5, false)
	src := createTestPDFBM(t, dir, 3, false)
	out := filepath.Join(dir, "inserted.pdf")
	if err := InsertFromPDF(base, 3, src, "2-3", out); err != nil {
		t.Fatalf("InsertFromPDF: %v", err)
	}
	n, _ := PageCount(out)
	if n != 7 {
		t.Fatalf("after insert = %d, want 7", n)
	}
}

func TestMergeAndBookmarks(t *testing.T) {
	dir := t.TempDir()
	a := createTestPDF(t, dir, 3)
	b := createTestPDF(t, dir, 2)
	out := filepath.Join(dir, "merged.pdf")
	if err := MergeFiles([]string{a, b}, out); err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	n, _ := PageCount(out)
	if n != 5 {
		t.Fatalf("merged pages = %d, want 5", n)
	}
	bms, err := ReadBookmarks(out)
	if err != nil {
		t.Fatalf("ReadBookmarks: %v", err)
	}
	// preserve 语义下应保留两份输入的书签
	if len(bms) < 2 {
		t.Fatalf("merged bookmarks = %d, want >= 2", len(bms))
	}
}

func TestRemoveBookmarks(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 3)
	out := filepath.Join(dir, "nobm.pdf")
	if err := RemoveBookmarks(p, out); err != nil {
		t.Fatalf("RemoveBookmarks: %v", err)
	}
	bms, _ := ReadBookmarks(out)
	if len(bms) != 0 {
		t.Fatalf("bookmarks after remove = %d, want 0", len(bms))
	}
}

func TestAttachments(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 2)
	// 附加一个文本附件
	attFile := filepath.Join(dir, "note.txt")
	_ = os.WriteFile(attFile, []byte("hello attachment"), 0o644)
	withAtt := filepath.Join(dir, "attached.pdf")
	if err := api.AddAttachmentsFile(p, withAtt, []string{attFile}, false, Config()); err != nil {
		t.Skipf("AddAttachments 不可用: %v", err)
	}
	aa, err := ListAttachments(withAtt)
	if err != nil || len(aa) != 1 {
		t.Fatalf("attachments = %d, %v; want 1", len(aa), err)
	}
	out := filepath.Join(dir, "noatt.pdf")
	if err := RemoveAttachments(withAtt, out, nil); err != nil {
		t.Fatalf("RemoveAttachments: %v", err)
	}
	aa, err = ListAttachments(out)
	if err != nil || len(aa) != 0 {
		t.Fatalf("attachments after remove = %d, %v; want 0", len(aa), err)
	}
}

func TestPageLabelsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 6)
	out := filepath.Join(dir, "labeled.pdf")
	spec := &PageLabelSpec{Ranges: []PageLabelRange{
		{StartPage: 0, Style: StyleLower, StartValue: 1},               // i..iii
		{StartPage: 3, Style: StyleDigit, Prefix: "A-", StartValue: 1}, // A-1..A-3
	}}
	if err := WritePageLabels(p, out, spec); err != nil {
		t.Fatalf("WritePageLabels: %v", err)
	}
	got, err := ReadPageLabels(out)
	if err != nil {
		t.Fatalf("ReadPageLabels: %v", err)
	}
	if got == nil || len(got.Ranges) != 2 {
		t.Fatalf("label ranges = %+v, want 2", got)
	}
	if got.Ranges[1].Prefix != "A-" || got.Ranges[1].Style != StyleDigit {
		t.Fatalf("range[1] = %+v", got.Ranges[1])
	}
}

func TestThumbnail(t *testing.T) {
	dir := t.TempDir()
	p := createTestPDF(t, dir, 2)
	outDir := filepath.Join(dir, "thumbs")
	n, err := RenderThumbnailsDir(p, outDir, 160, 1, nil)
	if err != nil || n != 2 {
		t.Fatalf("RenderThumbnailsDir = %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "p1.png")); err != nil {
		t.Fatalf("thumb p1.png missing: %v", err)
	}
}

func TestNaturalSort(t *testing.T) {
	dir := t.TempDir()
	// 引擎层不依赖该测试；留在此处覆盖 service 导出的行为在 service_test 中
	_ = dir
}
