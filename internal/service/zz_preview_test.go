package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pdfstudio/internal/engine"
)

func TestPreviewURLsAfterPlace(t *testing.T) {
	dir := t.TempDir()
	src := createPDFFile(t, dir, 3)
	src2 := createPDFFile(t, dir, 2)
	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	bigPath := func(p int) string {
		return filepath.Join(ds.store.ws.ThumbsDir(info.ID), fmt.Sprintf("big-p%d.png", p))
	}
	// 预渲染每页大图（模拟用户预览过）
	for p := 1; p <= 3; p++ {
		if _, err := ds.PageThumbnail(info.ID, p, 300); err != nil {
			t.Fatalf("预渲染 p%d: %v", p, err)
		}
	}
	// 导入 2 页并放置到第 1 页之前 → 新序：B1,B2,1,2,3
	tid, err := ds.Import(info.ID, ImportRequest{Paths: []string{src2}})
	if err != nil {
		t.Fatal(err)
	}
	bus := ds.bus
	waitTaskDone(t, bus, tid)
	if _, err := ds.PlaceBuffer(info.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	// 每页大图必须与“新序对应的原始页”渲染结果一致
	expected := map[int]int{1: -1, 2: -2, 3: 1, 4: 2, 5: 3} // 负数=缓冲区来源
	ref := func(p int) []byte {
		tmp := filepath.Join(dir, fmt.Sprintf("ref%d.png", p))
		if err := engine.RenderPagePNGToFile(src, p, 300, tmp); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(tmp)
		os.Remove(tmp)
		return b
	}
	refB := func(p int) []byte {
		tmp := filepath.Join(dir, fmt.Sprintf("refb%d.png", p))
		if err := engine.RenderPagePNGToFile(src2, p, 300, tmp); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(tmp)
		os.Remove(tmp)
		return b
	}
	for newP, oldP := range expected {
		if _, err := ds.PageThumbnail(info.ID, newP, 300); err != nil {
			t.Fatalf("大图 p%d: %v", newP, err)
		}
		got, err := os.ReadFile(bigPath(newP))
		if err != nil {
			t.Fatalf("大图 p%d 缺失: %v", newP, err)
		}
		var want []byte
		if oldP < 0 {
			want = refB(-oldP)
		} else {
			want = ref(oldP)
		}
		if string(got) != string(want) {
			t.Fatalf("大图 p%d 内容漂移：应来自原页 %d", newP, oldP)
		}
	}
}
