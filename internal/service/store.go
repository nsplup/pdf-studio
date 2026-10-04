package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv" // 新增
	"sync"
	"sync/atomic"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfstudio/internal/engine"
)

// Document 一个已打开文档的会话状态；同一文档写操作串行化。
type Document struct {
	ID         string
	SourcePath string // 用户原始文件，永不写入
	WorkPath   string // 当前工作版本
	PageCount  int
	Dims       []types.Dim

	mu      sync.Mutex
	version atomic.Int64
	store   *DocStore

	// buffer 当前挂起的导入内容（批量导入的图片/PDF 组成的临时 PDF），
	// 由前端通过 PlaceBuffer 放置到指定位置，或 DiscardBuffer 丢弃。
	buffer *ImportBuffer

	// att 附件会话：原文档附件的移除集合 + 新增附件（保存时合并写入）。
	att *AttachSession
	// res 页面内容资源 uuid 序列（逻辑顺序，保存时与 PageSeq 对齐）
	res []string
}

// ImportBuffer 挂起的导入内容；缩略图位于 /imgs/<ID>/pN.png。
type ImportBuffer struct {
	ID        string
	PDFPath   string
	ThumbsDir string
	PageCount int
	// Sources 导入清单中的 PDF 来源（含其在缓冲区内的 0-based 起始页），
	// 用于放置时合并页标签与附件。
	Sources []BufSource
}

// BufSource 缓冲区内的一个 PDF 来源片段。
type BufSource struct {
	Path  string // 原始文件路径
	Start int    // 在缓冲区 PDF 内的 0-based 起始页
	Count int    // 片段页数
}

// DocInfo 对前端暴露的文档信息。
type DocInfo struct {
	ID         string    `json:"id"`
	SourcePath string    `json:"sourcePath"`
	FileName   string    `json:"fileName"`
	PageCount  int       `json:"pageCount"`
	Pages      []PageDim `json:"pages"`
	HasThumbs  bool      `json:"hasThumbs"`
	PageRes    []string  `json:"pageRes,omitempty"`
}

// PageDim 页面尺寸（pt）。
type PageDim struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// DocStore 文档注册表。
type DocStore struct {
	ws   *Workspace
	docs sync.Map // id -> *Document
	seq  atomic.Int64
	// resReg 页面内容资源注册表：uuid -> pageSource（内容来源文件 + 页码）
	resReg sync.Map
}

func NewDocStore(ws *Workspace) *DocStore {
	return &DocStore{ws: ws}
}

// Create 从磁盘文件注册新文档会话。
func (s *DocStore) Create(sourcePath string) (*Document, error) {
	if sourcePath == "" {
		return nil, NewErr("INVALID_PARAM", "文件路径为空")
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, WrapErr("INVALID_PATH", "文件路径无效", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, WrapErr("FILE_NOT_FOUND", "文件不存在或无法访问", err)
	}
	if st.IsDir() {
		return nil, NewErr("INVALID_PARAM", "路径是目录而非文件")
	}
	count, err := engine.PageCount(abs)
	if err != nil {
		return nil, WrapErr("PDF_OPEN_FAILED", "打开 PDF 失败，文件可能损坏或不是有效 PDF", err)
	}
	dims, err := engine.PageDims(abs)
	if err != nil {
		dims = nil
	}

	id := fmt.Sprintf("doc-%d", s.seq.Add(1))
	doc := &Document{
		ID:         id,
		att:        &AttachSession{removed: map[string]bool{}},
		SourcePath: abs,
		WorkPath:   abs,
		PageCount:  count,
		Dims:       dims,
		store:      s,
	}
	if err := os.MkdirAll(s.ws.DocDir(id), 0o755); err != nil {
		return nil, WrapErr("WORKSPACE_CREATE", "初始化文档工作区失败", err)
	}
	s.docs.Store(id, doc)
	return doc, nil
}

// CreateBlank 注册一个无源文件的空白文档会话。
// 初始工作版本为一张 A4 空白页（仅占位）；页面内容随后由前端 pageSeq 完全覆盖。
func (s *DocStore) CreateBlank() (*Document, error) {
	id := fmt.Sprintf("doc-%d", s.seq.Add(1))
	if err := os.MkdirAll(s.ws.DocDir(id), 0o755); err != nil {
		return nil, WrapErr("WORKSPACE_CREATE", "初始化文档工作区失败", err)
	}
	doc := &Document{
		ID:    id,
		att:   &AttachSession{removed: map[string]bool{}},
		store: s,
		// SourcePath 故意留空：Save 会拒绝，须走 SaveAs
	}
	work := doc.nextVersion() // version 置 1，路径 = v1
	if err := engine.CreateBlankPagePDF(types.Dim{Width: 595, Height: 842}, work); err != nil {
		_ = os.RemoveAll(s.ws.DocDir(id))
		return nil, WrapErr("PDF_CREATE_FAILED", "创建空白文档失败", err)
	}
	doc.WorkPath = work
	if err := doc.refresh(); err != nil {
		_ = os.RemoveAll(s.ws.DocDir(id))
		return nil, err
	}
	s.docs.Store(id, doc)
	return doc, nil
}

// Get 获取文档会话。
func (s *DocStore) Get(id string) (*Document, error) {
	v, ok := s.docs.Load(id)
	if !ok {
		return nil, ErrDocNotFound(id)
	}
	return v.(*Document), nil
}

// Remove 关闭并移除文档会话。
func (s *DocStore) Remove(id string) {
	s.docs.Delete(id)
	if doc, err := s.Get(id); err == nil {
		_ = doc
	}
	s.ws.InvalidateThumbs(id)
	_ = os.RemoveAll(s.ws.DocDir(id))
}

// refresh 在写操作后刷新页数与尺寸。
// 缩略图由前端按受影响页增量处理，此处不再整册失效。
func (d *Document) refresh() error {
	count, err := engine.PageCount(d.WorkPath)
	if err != nil {
		return WrapErr("PDF_RELOAD_FAILED", "刷新文档状态失败", err)
	}
	dims, _ := engine.PageDims(d.WorkPath)
	d.PageCount = count
	d.Dims = dims
	return nil
}

// adoptWorkCopy 把装配完成的文件纳入版本链，作为新的工作版本。
func (d *Document) adoptWorkCopy(src string) error {
	newv := d.nextVersion()
	if err := copyFile(src, newv); err != nil {
		return WrapErr("PDF_RELOAD_FAILED", "更新工作版本失败", err)
	}
	d.WorkPath = newv
	return d.refresh()
}

// nextVersion 生成下一个版本文件路径。
func (d *Document) nextVersion() string {
	return d.store.ws.NewVersionPath(d.ID, d.version.Add(1))
}

// pageSnapshot 在锁内取得的一次性页面状态，用于后续锁外渲染。
type pageSnapshot struct {
	workPath  string
	pageCount int
	version   int64
}

// snapshot 持锁获取页面状态快照。渲染类调用者应使用快照字段，
// 不要直接读 d.WorkPath / d.PageCount，避免与写操作并发。
func (d *Document) snapshot() pageSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return pageSnapshot{
		workPath:  d.WorkPath,
		pageCount: d.PageCount,
		version:   d.version.Load(),
	}
}

// versionUnchanged 无锁判断当前版本是否仍等于快照时的版本。
// 版本在每次 nextVersion() 时递增，覆盖所有会改变页面内容的写操作。
func (d *Document) versionUnchanged(v int64) bool {
	return d.version.Load() == v
}

// docInfo 组装给前端的文档信息。适用于无锁调用方（自动加锁）。
func (d *Document) docInfo() *DocInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.docInfoLocked()
}

// hasAllPageThumbsLocked 判断 p1.png..pN.png 是否全部存在。
// 只看 p<正整数>.png 形式的条目，忽略 big-pN.png、*.tmp 等额外文件。
// 要求调用方已持有 d.mu。
func (d *Document) hasAllPageThumbsLocked() bool {
	if d.PageCount <= 0 {
		return false
	}
	entries, err := os.ReadDir(d.store.ws.ThumbsDir(d.ID))
	if err != nil {
		return false
	}
	present := make(map[int]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < 3 || name[0] != 'p' || name[len(name)-4:] != ".png" {
			continue
		}
		n, err := strconv.Atoi(name[1 : len(name)-4])
		if err != nil || n < 1 {
			continue
		}
		present[n] = struct{}{}
	}
	for p := 1; p <= d.PageCount; p++ {
		if _, ok := present[p]; !ok {
			return false
		}
	}
	return true
}

func (d *Document) docInfoLocked() *DocInfo {
	pages := make([]PageDim, 0, len(d.Dims))
	for _, dim := range d.Dims {
		pages = append(pages, PageDim{Width: dim.Width, Height: dim.Height})
	}
	name := "未命名.pdf"
	if d.SourcePath != "" {
		name = filepath.Base(d.SourcePath)
	}
	info := &DocInfo{
		ID:         d.ID,
		SourcePath: d.SourcePath,
		FileName:   name,
		PageCount:  d.PageCount,
		Pages:      pages,
	}
	info.HasThumbs = d.hasAllPageThumbsLocked()
	if len(d.res) == d.PageCount {
		info.PageRes = append([]string(nil), d.res...)
	}
	return info
}
