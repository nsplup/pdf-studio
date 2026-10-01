package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// tempSeq 为进程内临时文件命名提供单调递增序号。
var tempSeq atomic.Uint64

// TempPath 返回本进程本次调用唯一的临时文件路径。
//
// 命名包含 PID + 进程内序号 + 随机后缀，避免以下问题：
//   - 同一进程内不同任务使用同名固定文件互相覆盖；
//   - 多个应用实例共享系统临时目录时互踩；
//   - 固定名被其他本地用户预占（symlink/抢占）后本进程误用。
//
// 注意：返回值只是一个路径，不保证文件不存在。对安全性敏感的
// 场景应使用 TempFile 以 O_EXCL 语义原子创建。
func TempPath(name string) string {
	base := filepath.Base(name)
	if base == "" || base == "." || base == ".." {
		base = "tmp"
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	return filepath.Join(
		os.TempDir(),
		fmt.Sprintf(
			"pdfstudio-%d-%d-%s-%s",
			os.Getpid(),
			tempSeq.Add(1),
			hex.EncodeToString(r[:]),
			base,
		),
	)
}

// TempFile 在系统临时目录中原子创建唯一命名的文件，带 O_EXCL 语义，
// 阻止同名文件被预占（symlink 攻击）。调用方负责关闭与删除。
func TempFile(name string) (*os.File, error) {
	base := filepath.Base(name)
	if base == "" || base == "." || base == ".." {
		base = "tmp"
	}
	return os.CreateTemp(
		"",
		fmt.Sprintf("pdfstudio-%d-%s-*", os.Getpid(), base),
	)
}

func removeFile(p string) {
	if p == "" {
		return
	}
	_ = os.Remove(p)
}