package service

import (
	"os"
	"testing"

	"pdfstudio/internal/engine"
)

// TestSaveWithPageLabels 端到端：Open -> Save(带 labels) -> 验证源文件中的页标签已生效。
func TestSaveWithPageLabels(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 3)

	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	labels := []PageLabel{
		{StartPage: 0, Style: "r"},
		{StartPage: 1, Prefix: "A-", Style: "D", StartValue: 1},
	}
	if _, err := ds.Save(info.ID, &SaveOptions{Labels: &labels}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	spec, err := engine.ReadPageLabels(src)
	if err != nil {
		t.Fatalf("ReadPageLabels: %v", err)
	}
	if spec == nil || len(spec.Ranges) != 2 {
		t.Fatalf("保存后源文件页标签 = %+v, want 2 ranges", spec)
	}
	if spec.Ranges[0].Style != engine.StyleLower || spec.Ranges[1].Prefix != "A-" {
		t.Fatalf("页标签内容不符: %+v", spec.Ranges)
	}
}

// TestSaveReplacesExistingPagesLabels 对「已有页标签的文档」重新写入，验证新标签覆盖旧标签。
// 回归用例：pdfcpu Dict.Insert 为 insert-if-absent 语义，旧实现用 Insert 替换已有
// PageLabels 时会静默 no-op（无报错但不生效）。
func TestSaveReplacesExistingPagesLabels(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 3)

	// 预置：文件已有页标签
	old := &engine.PageLabelSpec{Ranges: []engine.PageLabelRange{
		{StartPage: 0, Prefix: "OLD-", Style: engine.StyleDigit},
	}}
	out := src + ".pre.pdf"
	if err := engine.WritePageLabels(src, out, old); err != nil {
		t.Fatalf("预置页标签失败: %v", err)
	}
	if err := os.Rename(out, src); err != nil {
		t.Fatalf("rename: %v", err)
	}

	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	labels := []PageLabel{{StartPage: 0, Prefix: "NEW-", Style: "R"}}
	if _, err := ds.Save(info.ID, &SaveOptions{Labels: &labels}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	spec, err := engine.ReadPageLabels(src)
	if err != nil {
		t.Fatalf("ReadPageLabels: %v", err)
	}
	if spec == nil || len(spec.Ranges) != 1 {
		t.Fatalf("覆盖写入后页标签 = %+v, want 1 range", spec)
	}
	if spec.Ranges[0].Prefix != "NEW-" || spec.Ranges[0].Style != engine.StyleUpper {
		t.Fatalf("页标签未被覆盖，仍为旧值: %+v", spec.Ranges)
	}
}

// TestSaveWithPageLabelsTwice 连续两次保存（带已有标签替换），验证保存链路幂等。
func TestSaveWithPageLabelsTwice(t *testing.T) {
	dir := t.TempDir()
	src := makePDF(t, dir, 3)

	ds, _, _, _ := newTestServices(t)
	info, err := ds.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	labels := []PageLabel{{StartPage: 0, Prefix: "P-", Style: "D"}}
	if _, err := ds.Save(info.ID, &SaveOptions{Labels: &labels}); err != nil {
		t.Fatalf("Save#1: %v", err)
	}
	labels2 := []PageLabel{{StartPage: 0, Style: "R"}, {StartPage: 2, Prefix: "C-", Style: "D"}}
	if _, err := ds.Save(info.ID, &SaveOptions{Labels: &labels2}); err != nil {
		t.Fatalf("Save#2: %v", err)
	}

	spec, err := engine.ReadPageLabels(src)
	if err != nil {
		t.Fatalf("ReadPageLabels: %v", err)
	}
	if spec == nil || len(spec.Ranges) != 2 {
		t.Fatalf("二次保存后页标签 = %+v, want 2 ranges", spec)
	}
	if spec.Ranges[0].Style != engine.StyleUpper || spec.Ranges[1].StartPage != 2 {
		t.Fatalf("二次保存内容不符: %+v", spec.Ranges)
	}
}
