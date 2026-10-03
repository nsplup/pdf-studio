package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pdfstudio/internal/engine"
)

// StoredAttachment 会话中新增的附件（文件已复制到工作区暂存目录）。
type StoredAttachment struct {
	Name     string
	Desc     string
	FilePath string
	ModTime  *time.Time
}

// AttachSession 附件会话：原文档附件的移除集合 + 新增附件。
// 保存 / 另存为时统一合并写入 PDF。
type AttachSession struct {
	mu      sync.Mutex
	removed map[string]bool
	added   []StoredAttachment
}

func (a *AttachSession) add(sa StoredAttachment) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 同名覆盖：先移除旧的同名新增项
	for i, old := range a.added {
		if old.Name == sa.Name {
			_ = os.Remove(old.FilePath)
			a.added = append(a.added[:i], a.added[i+1:]...)
			break
		}
	}
	a.added = append(a.added, sa)
}

// remove 移除：新增的直接删除，原文档附件记入移除集合。
func (a *AttachSession) remove(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, sa := range a.added {
		if sa.Name == name {
			a.added = append(a.added[:i], a.added[i+1:]...)
			_ = os.Remove(sa.FilePath)
			return
		}
	}
	a.removed[name] = true
}

// reset 清空会话状态：移除集合清空，新增项连同暂存文件一并删除。
// 保存成功后调用——此刻附件已经写入新的工作版本，会话不再需要保留任何变更。
func (a *AttachSession) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, sa := range a.added {
		_ = os.Remove(sa.FilePath)
	}
	a.removed = map[string]bool{}
	a.added = nil
}

func (a *AttachSession) hasChanges() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.removed) > 0 || len(a.added) > 0
}

// apply 把会话合并写入：in -> out（移除 + 新增）。
func (a *AttachSession) apply(inPath, outPath string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := inPath
	chain := []string{}
	defer func() {
		for _, p := range chain {
			_ = os.Remove(p)
		}
	}()
	if len(a.removed) > 0 {
		// 只移除确实存在的附件；pdfcpu 在名称树为空或名称不存在时会报错，
		// 而我们语义上是"确保这些附件不在输出里"，本来就该是幂等操作。
		orig, err := engine.ListAttachments(cur)
		if err != nil {
			return err
		}
		exist := make(map[string]bool, len(orig))
		for _, o := range orig {
			exist[o.FileName] = true
		}
		names := make([]string, 0, len(a.removed))
		for n := range a.removed {
			if exist[n] {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			mid := outPath + ".att0"
			if err := engine.RemoveAttachments(cur, mid, names); err != nil {
				return err
			}
			chain = append(chain, mid)
			cur = mid
		}
	}
	if len(a.added) > 0 {
		files := make([]string, 0, len(a.added))
		for _, sa := range a.added {
			files = append(files, sa.FilePath)
		}
		mid := outPath + ".att1"
		if err := engine.AddAttachments(cur, mid, files); err != nil {
			return err
		}
		chain = append(chain, mid)
		cur = mid
	}
	if cur == inPath {
		return nil
	}
	return os.Rename(cur, outPath)
}

// docAndAtt 取文档会话与附件会话。
func (s *MetaService) docAndAtt(id string) (*Document, *AttachSession, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if doc.att == nil {
		doc.att = &AttachSession{removed: map[string]bool{}}
	}
	return doc, doc.att, nil
}

// ListDocAttachments 列出附件会话：原文档附件（减去移除集合）+ 新增附件。
func (s *MetaService) ListDocAttachments(id string) ([]AttachmentInfo, error) {
	doc, att, err := s.docAndAtt(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	workPath := doc.WorkPath
	doc.mu.Unlock()

	orig, err := engine.ListAttachments(workPath)
	if err != nil {
		return nil, err
	}
	att.mu.Lock()
	defer att.mu.Unlock()
	out := make([]AttachmentInfo, 0, len(orig)+len(att.added))
	for _, a := range orig {
		if att.removed[a.FileName] {
			continue
		}
		out = append(out, AttachmentInfo{FileName: a.FileName, Description: a.Desc, ModTime: a.ModTime})
	}
	for _, sa := range att.added {
		out = append(out, AttachmentInfo{FileName: sa.Name, Description: sa.Desc, ModTime: sa.ModTime})
	}
	return out, nil
}

// AddDocAttachments 添加附件（复制到工作区暂存，保存时写入 PDF）。
func (s *MetaService) AddDocAttachments(id string, paths []string) ([]AttachmentInfo, error) {
	_, att, err := s.docAndAtt(id)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, NewErr("INVALID_PARAM", "未选择要添加的附件文件")
	}
	dir := s.store.ws.AttachDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, WrapErr("ATTACH_FAILED", "创建附件暂存目录失败", err)
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return nil, NewErr("FILE_NOT_FOUND", fmt.Sprintf("文件不存在: %s", filepath.Base(p)))
		}
		dst := filepath.Join(dir, randomToken()+"_"+filepath.Base(p))
		if err := copyFile(p, dst); err != nil {
			return nil, err
		}
		att.add(StoredAttachment{Name: filepath.Base(p), FilePath: dst})
	}
	return s.ListDocAttachments(id)
}

// RemoveDocAttachments 移除附件并返回最新列表。
func (s *MetaService) RemoveDocAttachments(id string, names []string) ([]AttachmentInfo, error) {
	_, att, err := s.docAndAtt(id)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		att.remove(n)
	}
	return s.ListDocAttachments(id)
}

// ClearDocAttachments 清空附件会话（移除全部原文档附件 + 全部新增）。
func (s *MetaService) ClearDocAttachments(id string) ([]AttachmentInfo, error) {
	doc, att, err := s.docAndAtt(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	workPath := doc.WorkPath
	doc.mu.Unlock()

	if orig, err := engine.ListAttachments(workPath); err == nil {
		for _, a := range orig {
			att.remove(a.FileName)
		}
	}
	att.mu.Lock()
	added := att.added
	att.added = nil
	att.mu.Unlock()
	for _, sa := range added {
		_ = os.Remove(sa.FilePath)
	}
	return s.ListDocAttachments(id)
}
