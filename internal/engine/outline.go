package engine

import (
	"fmt"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

// ReadBookmarks 读取书签树（含层级）。
func ReadBookmarks(path string) ([]pdfcpu.Bookmark, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()
	bms, err := api.Bookmarks(f, Config())
	if err != nil {
		return nil, fmt.Errorf("读取书签失败: %w", err)
	}
	return bms, nil
}

// WriteBookmarks 将书签树写入新文件；replace=true 时替换全部既有书签，false 为追加。
func WriteBookmarks(inPath, outPath string, bms []pdfcpu.Bookmark, replace bool) error {
	if err := api.AddBookmarksFile(inPath, outPath, bms, replace, Config()); err != nil {
		return fmt.Errorf("写入书签失败: %w", err)
	}
	return nil
}

// RemoveBookmarks 移除全部书签/目录。
func RemoveBookmarks(inPath, outPath string) error {
	if err := api.RemoveBookmarksFile(inPath, outPath, Config()); err != nil {
		return fmt.Errorf("移除书签失败: %w", err)
	}
	return nil
}

// ExportBookmarksJSON 将书签树导出为 pdfcpu JSON 目录文件。
func ExportBookmarksJSON(inPath, jsonOutPath string) error {
	if err := api.ExportBookmarksFile(inPath, jsonOutPath, Config()); err != nil {
		return fmt.Errorf("导出目录失败: %w", err)
	}
	return nil
}

// ImportBookmarksJSON 从 pdfcpu JSON 文件导入书签目录。
func ImportBookmarksJSON(inPath, jsonInPath, outPath string, replace bool) error {
	if err := api.ImportBookmarksFile(inPath, jsonInPath, outPath, replace, Config()); err != nil {
		return fmt.Errorf("导入目录失败: %w", err)
	}
	return nil
}
