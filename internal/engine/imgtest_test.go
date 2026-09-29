package engine

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImportImagesReal(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "a.png")
	f, _ := os.Create(imgPath)
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for x := 0; x < 200; x++ {
		for y := 0; y < 100; y++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	f.Close()

	out := filepath.Join(dir, "out.pdf")
	err := ImportImagesToPDF([]string{imgPath}, out, ImageImportConfig{PageSize: PageSizeA4, Pos: "full"})
	if err != nil {
		t.Fatalf("ImportImagesToPDF: %v", err)
	}
	n, err := PageCount(out)
	if err != nil || n != 1 {
		t.Fatalf("pages=%d err=%v", n, err)
	}

	out2 := filepath.Join(dir, "out2.pdf")
	if err := ImportImagesToPDF([]string{imgPath}, out2, ImageImportConfig{PageSize: PageSizeAuto, MarginPt: 10}); err != nil {
		t.Fatalf("Auto: %v", err)
	}
}
