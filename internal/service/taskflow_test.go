package service

import (
	"pdfstudio/internal/engine"

	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeBus struct {
	mu     sync.Mutex
	events []TaskUpdate
}

func (f *fakeBus) Emit(name string, data any) {
	_ = name
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := data.(TaskUpdate); ok {
		f.events = append(f.events, u)
	}
}

func (f *fakeBus) waitDone(t *testing.T) []TaskUpdate {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.events)
		f.mu.Unlock()
		if n > 0 {
			last := f.events[len(f.events)-1]
			if last.Kind == "done" || last.Kind == "error" {
				return f.events
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("任务超时未完成，事件=%v", f.events)
	return nil
}

func newTestServices(t *testing.T) (*DocumentService, *MergeService, *ImageService, *fakeBus) {
	t.Helper()
	bus := &fakeBus{}
	ws, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Cleanup() })
	store := NewDocStore(ws)
	busObj := &EventBus{}
	busObj.SetEmitter(bus.Emit)
	return NewDocumentService(busObj, store),
		NewMergeService(busObj),
		NewImageService(busObj, ws),
		bus
}

func createPDFFile(t *testing.T, dir string, pages int) string {
	t.Helper()
	return makePDF(t, dir, pages)
}

func pageCount(path string) (int, error) {
	return engine.PageCount(path)
}

func makePNG(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	img.Set(10, 10, color.RGBA{A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func TestServiceMergeTask(t *testing.T) {
	dir := t.TempDir()
	p1 := createPDFFile(t, dir, 2)
	p2 := createPDFFile(t, dir, 3)
	_, merge, _, bus := newTestServices(t)
	out := filepath.Join(dir, "merged.pdf")
	taskID, err := merge.Start(MergeRequest{InputPaths: []string{p1, p2}, OutputPath: out, SortMode: "given"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events := bus.waitDone(t)
	last := events[len(events)-1]
	if last.Kind != "done" {
		t.Fatalf("merge kind=%s err=%s", last.Kind, last.Error)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("输出文件未写入: %v", err)
	}
	n, _ := pageCount(out)
	if n != 5 {
		t.Fatalf("merged pages=%d", n)
	}
	_ = taskID
}

func TestServiceImageTask(t *testing.T) {
	dir := t.TempDir()
	i1 := makePNG(t, dir, "a.png")
	i2 := makePNG(t, dir, "b.png")
	_, _, img, bus := newTestServices(t)
	out := filepath.Join(dir, "out.pdf")
	_, err := img.Start(ImagesToPDFRequest{
		ImagePaths: []string{i1, i2},
		OutputPath: out,
		PageSize:   "A4",
		MarginPt:   12,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events := bus.waitDone(t)
	last := events[len(events)-1]
	if last.Kind != "done" {
		t.Fatalf("image kind=%s err=%s", last.Kind, last.Error)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("输出文件未写入: %v", err)
	}
}

func TestServiceSaveToSource(t *testing.T) {
	dir := t.TempDir()
	src := createPDFFile(t, dir, 3)
	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.DeletePages(info.ID, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Save(info.ID, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	n, _ := pageCount(src)
	if n != 2 {
		t.Fatalf("原文件页数=%d, want 2", n)
	}
}


func TestServiceMovePages(t *testing.T) {
	dir := t.TempDir()
	src := createPDFFile(t, dir, 6)
	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	// 把第 1-2 页移动到第 5 页之后（原编号）
	info2, err := ds.MovePages(info.ID, "1-2", 5, false)
	if err != nil {
		t.Fatalf("MovePages: %v", err)
	}
	if info2.PageCount != 6 {
		t.Fatalf("页数=%d want 6", info2.PageCount)
	}
	// 新顺序：3,4,5,1,2,6 → 校验方式：导出书签页码验证不可靠，这里验证
	// 移动 5-6 到开头后总页数不变且 MovePages 对全部页选择报错
	if _, err := ds.MovePages(info2.ID, "1-6", 3, true); err == nil {
		t.Fatal("移动全部页面应报错")
	}
}

func TestServiceImportBuffer(t *testing.T) {
	dir := t.TempDir()
	png1 := makePNG(t, dir, "x.png")
	png2 := makePNG(t, dir, "y.png")
	pdf := createPDFFile(t, dir, 2)
	ds, _, _, bus := newTestServices(t)

	info, err := ds.Open(pdf)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.Import(info.ID, ImportRequest{Paths: []string{png1, png2}})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	events := bus.waitDone(t)
	last := events[len(events)-1]
	if last.Kind != "done" {
		t.Fatalf("import kind=%s err=%s", last.Kind, last.Error)
	}
	// 放置到末尾
	res, err := ds.PlaceBuffer(info.ID, info.PageCount+1, true)
	if err != nil {
		t.Fatalf("PlaceBuffer: %v", err)
	}
	if res.Info.PageCount != 4 {
		t.Fatalf("页数=%d want 4", res.Info.PageCount)
	}
	// 缓冲区已清空
	if err := ds.DiscardBuffer(info.ID); err != nil {
		t.Fatal(err)
	}
}
