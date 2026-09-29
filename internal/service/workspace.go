package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Workspace 管理应用临时目录：工作版本、缩略图缓存，退出时统一清理。
type Workspace struct {
	root string
}

func NewWorkspace() (*Workspace, error) {
	root, err := os.MkdirTemp("", "pdfstudio-*")
	if err != nil {
		return nil, WrapErr("WORKSPACE_CREATE", "创建临时工作区失败", err)
	}
	return &Workspace{root: root}, nil
}

func (w *Workspace) Root() string { return w.root }

// DocDir 返回文档工作版本目录。
func (w *Workspace) DocDir(docID string) string {
	return filepath.Join(w.root, "docs", docID)
}

// ImagesDir 返回图片会话资源目录（/imgs/<sessionID>/ 静态路由对应）。
func (w *Workspace) ImagesDir(sessionID string) string {
	return filepath.Join(w.root, "imgs", sessionID)
}

// ThumbsDir 返回文档缩略图缓存目录（与静态资源路由一致）。
func (w *Workspace) ThumbsDir(docID string) string {
	return filepath.Join(w.root, "thumbs", docID)
}
// ResourceDir 页面内容资源的根目录：<ws>/thumbs/<uuid>/。
func (w *Workspace) ResourceDir(uuid string) string {
	return filepath.Join(w.root, "thumbs", uuid)
}


// InvalidateThumbs 删除文档缩略图缓存（页面编辑后调用）。
func (w *Workspace) InvalidateThumbs(docID string) {
	_ = os.RemoveAll(w.ThumbsDir(docID))
}

// NewVersionPath 返回下一个工作版本文件路径。
func (w *Workspace) NewVersionPath(docID string, seq int64) string {
	return filepath.Join(w.DocDir(docID), fmt.Sprintf("v%d.pdf", seq))
}

// Cleanup 清理整个临时工作区，应用退出时调用。
func (w *Workspace) Cleanup() {
	_ = os.RemoveAll(w.root)
}

// AttachDir 返回文档附件暂存目录（新增附件文件与导入提取的附件）。
func (w *Workspace) AttachDir(docID string) string {
	return filepath.Join(w.root, "attach", docID)
}

// RearrangeThumbs 按映射重排缩略图文件（纯文件操作，零重渲染）：
//   - count：新总页数
//   - perm：新页号(1-based) -> 旧页号(1-based)，内容未变仅位置移动
//   - copyFrom：新页号 -> 源文件路径（导入缓冲区已渲染好的预览图）
//
// 未覆盖的新页号不生成文件（后续按需渲染兜底）；目录内残留旧文件全部清理。
// 缩略图目录不存在时静默返回（尚未生成过缩略图）。
func (w *Workspace) RearrangeThumbs(docID string, count int, perm map[int]int, copyFrom map[int]string) error {
	dir := w.ThumbsDir(docID)
	if _, err := os.Stat(dir); err != nil {
		return nil // 尚未生成过缩略图
	}
	// 两阶段替换：先把所有源文件暂存为 .tmp（源在暂存期间不被覆盖），
	// 再统一 rename 到最终槽位。杜绝同目录就地复制造成的链式覆盖漂移。
	type job struct{ src, dst string }
	plan := make([]job, 0, count*2)
	for newPage := 1; newPage <= count; newPage++ {
		// 小图
		dst := filepath.Join(dir, fmt.Sprintf("p%d.png", newPage))
		if old, ok := perm[newPage]; ok && old > 0 {
			plan = append(plan, job{filepath.Join(dir, fmt.Sprintf("p%d.png", old)), dst})
		} else if s, ok := copyFrom[newPage]; ok && s != "" {
			plan = append(plan, job{s, dst})
		} else {
			plan = append(plan, job{"", dst})
		}
		// 大图（预览）
		bigDst := filepath.Join(dir, fmt.Sprintf("big-p%d.png", newPage))
		if old, ok := perm[newPage]; ok && old > 0 {
			plan = append(plan, job{filepath.Join(dir, fmt.Sprintf("big-p%d.png", old)), bigDst})
		} else {
			plan = append(plan, job{"", bigDst})
		}
	}
	// 阶段一：暂存（源文件此刻均未被修改）
	staged := 0
	for i, j := range plan {
		if j.src == "" {
			continue
		}
		if err := copyFileTo(j.src, j.dst+".tmp"); err != nil {
			// 源缺失：目标槽位清空，交由按需渲染自愈
			if os.IsNotExist(err) {
				plan[i].src = ""
				_ = os.Remove(j.dst)
				continue
			}
			for _, j2 := range plan {
				_ = os.Remove(j2.dst + ".tmp")
			}
			return err
		}
		plan[i].dst = j.dst
		staged++
	}
	// 阶段二：统一替换
	for _, j := range plan {
		_ = os.Remove(j.dst) // Windows：rename 不能覆盖已存在文件
		if j.src != "" && staged >= 0 {
			if err := os.Rename(j.dst+".tmp", j.dst); err != nil {
				_ = copyFileTo(j.dst+".tmp", j.dst)
				_ = os.Remove(j.dst + ".tmp")
			}
		} else {
			_ = os.Remove(j.dst + ".tmp")
		}
	}
	// 清理超出页数的残留槽位与散落临时文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "big-"), ".png")
		if base == name && !strings.HasPrefix(name, "p") {
			continue
		}
		n := 0
		if strings.HasPrefix(base, "p") {
			fmt.Sscanf(strings.TrimPrefix(base, "p"), "%d", &n)
		}
		if n > count {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

// replaceFile 把 src 覆盖到 dst（src 为空或缺失时清空 dst 槽位）。
func replaceFile(src, dst string) error {
	if src == "" {
		_ = os.Remove(dst)
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(dst) // 源缺失：清槽，交由按需渲染自愈
			return nil
		}
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Windows 不允许 rename 覆盖已存在文件，先移除目标
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		// 兜底：直接复制
		if err2 := copyFileTo(tmp, dst); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
		_ = os.Remove(tmp)
	}
	return nil
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(in)
	return err
}

// NewTempSubdir 在工作区创建一次性子目录（调用方负责清理）。
func (w *Workspace) NewTempSubdir(name string) (string, error) {
	dir := filepath.Join(w.root, "tmp", fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
