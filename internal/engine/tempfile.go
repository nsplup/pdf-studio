package engine

import (
	"os"
	"path/filepath"
)

// TempPath 返回本进程唯一命名的临时文件路径（系统临时目录下）。
func TempPath(name string) string {
	dir := os.TempDir()
	base := filepath.Base(name)
	return filepath.Join(dir, "pdfstudio-"+base)
}

func removeFile(p string) {
	if p == "" {
		return
	}
	_ = os.Remove(p)
}
