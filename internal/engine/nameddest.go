package engine

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// stripNamedDests 复制 PDF 并移除目录中的命名目标树（/Names 与 /Dests）。
// 某些含命名目标的文档在抽取/合并页时触发 pdfcpu 内部迁移缺陷，
// 移除后可安全执行页级操作；副作用是 GoTo 链接退化为直接页引用或失效。
func stripNamedDests(inPath, outPath string) error {
	ctx, err := api.ReadContextFile(inPath)
	if err != nil {
		return fmt.Errorf("读取文档失败: %w", err)
	}
	delete(ctx.RootDict, "Names")
	delete(ctx.RootDict, "Dests")
	ctx.Names = nil
	if err := api.WriteContextFile(ctx, outPath); err != nil {
		return fmt.Errorf("写出文档失败: %w", err)
	}
	return nil
}

// ensureStripped 确保文件无命名目标树：已是目标文件则原样返回。
func ensureStripped(inPath, outPath string) error {
	if inPath == outPath {
		return nil
	}
	return stripNamedDests(inPath, outPath)
}
