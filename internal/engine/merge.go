package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// MergeFiles 将多个 PDF 按给定顺序合并为 outPath。
// pdfcpu 合并默认保留各文件的书签树（preserve 语义），页码偏移自动处理。
func MergeFiles(inFiles []string, outPath string) error {
	if len(inFiles) < 1 {
		return fmt.Errorf("至少需要一个输入文件")
	}
	if err := api.MergeCreateFile(inFiles, outPath, false, Config()); err != nil {
		// 命名目标迁移缺陷兜底：剥离各输入的命名目标树后重试
		cleaned := make([]string, 0, len(inFiles))
		dir := filepath.Dir(outPath)
		for i, f := range inFiles {
			c := filepath.Join(dir, fmt.Sprintf(".mc-%d-%d.pdf", time.Now().UnixNano(), i))
			if err := stripNamedDests(f, c); err != nil {
				cleanupFile(c)
				return fmt.Errorf("合并失败: %w", err)
			}
			cleaned = append(cleaned, c)
		}
		defer func() {
			for _, c := range cleaned {
				cleanupFile(c)
			}
		}()
		if err := api.MergeCreateFile(cleaned, outPath, false, Config()); err != nil {
			return fmt.Errorf("合并失败: %w", err)
		}
	}
	return nil
}

func cleanupFile(p string) { _ = os.Remove(p) }
