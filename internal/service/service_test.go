package service

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func init() { api.DisableConfigDir() }

// makePDF 生成带书签的多页测试 PDF（供服务层测试使用）。
func makePDF(t *testing.T, dir string, pages int) string {
	t.Helper()
	mediaBox := types.RectForFormat("A4")
	page := model.Page{MediaBox: mediaBox, Fm: model.FontMap{}, Buf: new(bytes.Buffer)}
	pdfcpu.CreateTestPageContent(page)
	xRefTable, err := pdfcpu.CreateDemoXRef()
	if err != nil {
		t.Fatal(err)
	}
	rootDict, err := xRefTable.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := pdfcpu.AddPageTreeWithSamplePage(xRefTable, rootDict, page); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, fmt.Sprintf("m%d.pdf", pages))
	if err := api.CreatePDFFile(xRefTable, base, nil); err != nil {
		t.Fatal(err)
	}
	if pages > 1 {
		grown := filepath.Join(dir, fmt.Sprintf("mg%d.pdf", pages))
		for i := 0; i < pages-1; i++ {
			out := filepath.Join(dir, fmt.Sprintf("mg%d_%d.pdf", pages, i))
			if i == pages-2 {
				out = grown
			}
			if err := api.InsertPagesFile(base, out, []string{"1"}, true, nil, nil); err != nil {
				t.Fatal(err)
			}
			base = out
		}
		base = grown
	}
	bms := []pdfcpu.Bookmark{}
	for i := 0; i < pages; i++ {
		bms = append(bms, pdfcpu.Bookmark{Title: fmt.Sprintf("B%d", i+1), PageFrom: i + 1})
	}
	out := filepath.Join(dir, fmt.Sprintf("bm%d.pdf", pages))
	if err := api.AddBookmarksFile(base, out, bms, true, nil); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNaturalSortOrder(t *testing.T) {
	in := []string{"part_10.pdf", "part_2.pdf", "part_1.pdf"}
	NaturalSortStrings(in)
	want := []string{"part_1.pdf", "part_2.pdf", "part_10.pdf"}
	for i := range want {
		if filepath.Base(in[i]) != want[i] {
			t.Fatalf("order = %v, want %v", in, want)
		}
	}
	if !NaturalLess("a2", "a10") || NaturalLess("a10", "a2") {
		t.Fatal("NaturalLess 结果错误")
	}
}

func TestDocumentSessionFlow(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 5)

	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	bus := NewEventBus()
	var mu sync.Mutex
	var events []TaskUpdate
	bus.SetEmitter(func(name string, data any) {
		mu.Lock()
		defer mu.Unlock()
		if u, ok := data.(TaskUpdate); ok {
			events = append(events, u)
		}
	})

	store := NewDocStore(ws)
	ds := NewDocumentService(bus, store)

	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if info.PageCount != 5 {
		t.Fatalf("pages = %d, want 5", info.PageCount)
	}

	// 删除页
	info, err = ds.DeletePages(info.ID, "1,2")
	if err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	if info.PageCount != 3 {
		t.Fatalf("after delete pages = %d, want 3", info.PageCount)
	}

	// 导入另一个 PDF 的页面（缓冲区 → 放置到第 1 页之前）
	src2 := makePDF(t, dir, 2)
	tid, err := ds.Import(info.ID, ImportRequest{Paths: []string{src2}})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	waitTaskDone(t, bus, tid)
	res, err := ds.PlaceBuffer(info.ID, 1, true)
	if err != nil {
		t.Fatalf("PlaceBuffer: %v", err)
	}
	info = res.Info
	if info.PageCount != 5 {
		t.Fatalf("after place pages = %d, want 5", info.PageCount)
	}

	// 书签读取
	osvc := NewOutlineService(bus)
	if _, err := osvc.Read(info.SourcePath); err != nil {
		t.Logf("Read 原书签: %v", err)
	}

	// 另存
	savePath := filepath.Join(dir, "saved.pdf")
	if _, err := ds.SaveAs(info.ID, savePath, nil); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if n, err := api.PageCountFile(savePath); err != nil || n != 5 {
		t.Fatalf("saved pages = %d, %v; want 5", n, err)
	}

	// 缩略图任务（同步等待事件）
	tid, err = ds.GenerateThumbnails(info.ID, 120, 1)
	if err != nil {
		t.Fatalf("GenerateThumbnails: %v", err)
	}
	waitTaskDone(t, bus, tid)
}

func TestMergeService(t *testing.T) {
	dir := t.TempDir()
	a := makePDF(t, dir, 3)
	b := makePDF(t, dir, 2)

	ws, _ := NewWorkspace()
	defer ws.Cleanup()
	bus := NewEventBus()
	bus.SetEmitter(func(name string, data any) {})
	ms := NewMergeService(bus)

	out := filepath.Join(dir, "merged.pdf")
	taskID, err := ms.Start(MergeRequest{InputPaths: []string{a, b}, OutputPath: out, SortMode: "given"})
	if err != nil {
		t.Fatalf("Merge Start: %v", err)
	}
	if taskID == "" {
		t.Fatal("taskID 为空")
	}
	waitTaskDone(t, bus, taskID)
	if n, err := api.PageCountFile(out); err != nil || n != 5 {
		t.Fatalf("merged pages = %d, %v; want 5", n, err)
	}
}

func TestImageService(t *testing.T) {
	dir := t.TempDir()
	// 生成两张 PNG 测试图
	img1 := filepath.Join(dir, "a.png")
	img2 := filepath.Join(dir, "b.png")
	if err := writeTestPNG(img1, 200, 100); err != nil {
		t.Fatal(err)
	}
	if err := writeTestPNG(img2, 300, 300); err != nil {
		t.Fatal(err)
	}
	ws, _ := NewWorkspace()
	defer ws.Cleanup()
	bus := NewEventBus()
	bus.SetEmitter(func(name string, data any) {})
	is := NewImageService(bus, ws)
	out := filepath.Join(dir, "imgs.pdf")
	taskID, err := is.Start(ImagesToPDFRequest{
		ImagePaths: []string{img1, img2},
		OutputPath: out,
		PageSize:   "A4",
		MarginPt:   20,
	})
	if err != nil {
		t.Fatalf("Images Start: %v", err)
	}
	waitTaskDone(t, bus, taskID)
	if n, err := api.PageCountFile(out); err != nil || n != 2 {
		t.Fatalf("img pdf pages = %d, %v; want 2", n, err)
	}
}

func TestMetaService(t *testing.T) {
	dir := t.TempDir()
	p := makePDF(t, dir, 2)
	ws, _ := NewWorkspace()
	defer ws.Cleanup()
	bus := NewEventBus()
	store := NewDocStore(ws)
	ms := NewMetaService(bus, store)

	labels, err := ms.ReadPageLabels(p)
	if err != nil {
		t.Fatalf("ReadPageLabels: %v", err)
	}
	if labels != nil {
		t.Fatalf("无标签文档返回 %v, want nil", labels)
	}
	out := filepath.Join(dir, "labeled.pdf")
	err = writeLabelsFile(p, out, []PageLabel{
		{StartPage: 0, Style: "r"},
		{StartPage: 1, Prefix: "A-", Style: "D", StartValue: 1},
	})
	if err != nil {
		t.Fatalf("WritePageLabels: %v", err)
	}
	labels, err = ms.ReadPageLabels(out)
	if err != nil || len(labels) != 2 {
		t.Fatalf("labels = %v, %v", labels, err)
	}
}

func waitMs(ms int) { timeSleep(ms) }

// waitTaskDone 轮询等待任务完成事件。
func waitTaskDone(t *testing.T, bus *EventBus, taskID string) {
	t.Helper()
	c := make(chan TaskUpdate, 64)
	bus.SetEmitter(func(name string, data any) {
		if u, ok := data.(TaskUpdate); ok && u.TaskID == taskID {
			select {
			case c <- u:
			default:
			}
		}
	})
	defer bus.SetEmitter(func(string, any) {})
	deadline := 200
	for {
		select {
		case u := <-c:
			if u.Kind == "done" {
				return
			}
			if u.Kind == "error" {
				t.Fatalf("任务失败: %s", u.Error)
			}
		default:
		}
		if deadline--; deadline <= 0 {
			t.Fatal("任务超时")
		}
		waitMs(50)
	}
}

func TestThumbsRearrangeAfterOps(t *testing.T) {
	dir := t.TempDir()
	src := createPDFFile(t, dir, 5)
	src2 := createPDFFile(t, dir, 2)
	ds, _, _, _ := newTestServices(t)
	ws := ds.store.ws
	bus := ds.bus
	info, err := ds.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	// 先生成缩略图（同步任务）
	tid, err := ds.GenerateThumbnails(info.ID, 120, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskDone(t, bus, tid)

	readThumb := func(p int) string {
		b, err := os.ReadFile(filepath.Join(ws.ThumbsDir(info.ID), fmt.Sprintf("p%d.png", p)))
		if err != nil {
			t.Fatalf("p%d.png 读取失败: %v", p, err)
		}
		return string(b)
	}
	// 用页号标记文件内容：以缩略图存在性 + 数量校验为主
	n := 5
	// 移动 1-2 到末页之后（atIndex=5, before=false）→ 新序 3,4,5,1,2
	if _, err := ds.MovePages(info.ID, "1-2", 5, false); err != nil {
		t.Fatalf("MovePages: %v", err)
	}
	for i := 1; i <= n; i++ {
		if _, err := os.Stat(filepath.Join(ws.ThumbsDir(info.ID), fmt.Sprintf("p%d.png", i))); err != nil {
			t.Fatalf("移动后 p%d.png 缺失", i)
		}
	}
	// 删除第 1 页 → 4 页，全部存在
	if _, err := ds.DeletePages(info.ID, "1"); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	for i := 1; i <= n-1; i++ {
		if _, err := os.Stat(filepath.Join(ws.ThumbsDir(info.ID), fmt.Sprintf("p%d.png", i))); err != nil {
			t.Fatalf("删除后 p%d.png 缺失", i)
		}
	}
	// 导入 2 页并放置到第 1 页之前 → 6 页，第 1-2 页为缓冲区预览
	tid, err = ds.Import(info.ID, ImportRequest{Paths: []string{src2}})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	waitTaskDone(t, bus, tid)
	res, err := ds.PlaceBuffer(info.ID, 1, true)
	if err != nil {
		t.Fatalf("PlaceBuffer: %v", err)
	}
	if res.Info.PageCount != 6 {
		t.Fatalf("放置后页数=%d, want 6", res.Info.PageCount)
	}
	for i := 1; i <= 6; i++ {
		if _, err := os.Stat(filepath.Join(ws.ThumbsDir(info.ID), fmt.Sprintf("p%d.png", i))); err != nil {
			t.Fatalf("放置后 p%d.png 缺失", i)
		}
	}
	// 插入空白页（第 3 页前）→ 7 页
	if _, err := ds.InsertBlankPages(info.ID, 3, 1, true); err != nil {
		t.Fatalf("InsertBlankPages: %v", err)
	}
	for i := 1; i <= 7; i++ {
		if _, err := os.Stat(filepath.Join(ws.ThumbsDir(info.ID), fmt.Sprintf("p%d.png", i))); err != nil {
			t.Fatalf("空白页后 p%d.png 缺失", i)
		}
	}
	_ = readThumb
}
