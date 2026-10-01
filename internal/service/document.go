package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"pdfstudio/internal/engine"
)

// DocumentService 页面编辑：打开、增删页、插入 PDF 页面、另存。
type DocumentService struct {
	bus   *EventBus
	store *DocStore
}

func NewDocumentService(bus *EventBus, store *DocStore) *DocumentService {
	return &DocumentService{bus: bus, store: store}
}

// Open 打开 PDF 并注册会话。
func (s *DocumentService) Open(path string) (*DocInfo, error) {
	doc, err := s.store.Create(path)
	if err != nil {
		return nil, err
	}
	return doc.docInfo(), nil
}

// Info 查询文档状态。
func (s *DocumentService) Info(id string) (*DocInfo, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	return doc.docInfo(), nil
}

// Close 关闭文档会话并清理临时数据。
func (s *DocumentService) Close(id string) error {
	if _, err := s.store.Get(id); err != nil {
		return err
	}
	s.store.Remove(id)
	return nil
}

// GenerateThumbnails 异步生成缩略图（fromPage 之前的页保留缓存，增量重渲染）；
// 完成事件 result 为静态资源前缀 "/thumbs/<id>"。
func (s *DocumentService) GenerateThumbnails(id string, width int, fromPage int) (string, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	if width <= 0 || width > 600 {
		width = 200
	}
	taskID := "thumbs-" + id
	doc.mu.Lock()
	workPath, pageCount := doc.WorkPath, doc.PageCount
	doc.mu.Unlock()

	if fromPage < 1 {
		fromPage = 1
	}
	if fromPage > pageCount {
		return "", NewErr("INVALID_PARAM", fmt.Sprintf("起始页 %d 超出文档页数 %d", fromPage, pageCount))
	}
	s.bus.Task(taskID, "生成缩略图", func(report func(current, total int, msg string)) (any, error) {
		outDir := s.store.ws.ThumbsDir(id)
		n, err := engine.RenderThumbnailsDir(workPath, outDir, width, fromPage, func(done, total int) {
			report(done, total, fmt.Sprintf("渲染缩略图 %d/%d", done, total))
		})
		if err != nil {
			// 部分成功也保留已生成页
			if n < fromPage {
				return nil, WrapErr("THUMB_RENDER_FAILED", "生成缩略图失败", err)
			}
		}
		return "/thumbs/" + id, nil
	})
	return taskID, nil
}

// InsertBlankPages 在 pageIndex 页前/后插入 count 张空白页。
// 空白页尺寸自动匹配锚点页（1-based；pageIndex 可为 PageCount+1 追加到末尾）。
// DeletePages 删除页面选择表达式指定的页（如 "2,5-7"）。
// PageThumbnail 同步渲染单页大图（用于预览浮层），返回可访问 URL。
// 缓存于 <thumbs>/big-p<N>.png；文件随重排被清理后在下次请求时重渲染。
func (s *DocumentService) PageThumbnail(id string, pageNr, width int) (string, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	if width <= 0 || width > 1400 {
		width = 900
	}
	if pageNr < 1 || pageNr > doc.PageCount {
		return "", NewErr("INVALID_PARAM", fmt.Sprintf("页码 %d 超出范围 (1-%d)", pageNr, doc.PageCount))
	}
	out := filepath.Join(s.store.ws.ThumbsDir(id), fmt.Sprintf("big-p%d.png", pageNr))
	if _, err := os.Stat(out); err == nil {
		return "/thumbs/" + id + "/big-p" + fmt.Sprint(pageNr) + ".png", nil
	}
	if err := os.MkdirAll(s.store.ws.ThumbsDir(id), 0o755); err != nil {
		return "", WrapErr("THUMB_RENDER_FAILED", "创建缩略图目录失败", err)
	}
	if err := engine.RenderPagePNGToFile(doc.WorkPath, pageNr, width, out); err != nil {
		_ = os.Remove(out)
		return "", WrapErr("THUMB_RENDER_FAILED", "渲染预览失败", err)
	}
	return "/thumbs/" + id + "/big-p" + fmt.Sprint(pageNr) + ".png", nil
}

// EnsurePageThumb 确保第 page 页的 180px 缩略图存在，缺失则同步渲染（自愈）。
func (s *DocumentService) EnsurePageThumb(id string, page int) error {
	doc, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if page < 1 || page > doc.PageCount {
		return NewErr("INVALID_PARAM", fmt.Sprintf("页码 %d 超出范围 (1-%d)", page, doc.PageCount))
	}
	out := filepath.Join(s.store.ws.ThumbsDir(id), fmt.Sprintf("p%d.png", page))
	if _, err := os.Stat(out); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.store.ws.ThumbsDir(id), 0o755); err != nil {
		return err
	}
	doc.mu.Lock()
	workPath := doc.WorkPath
	doc.mu.Unlock()
	tmp := out + ".tmp"
	if err := engine.RenderPagePNGToFile(workPath, page, 180, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(out)
	return os.Rename(tmp, out)
}

// InsertBlankPages 在 atIndex 页前/后插入 count 张空白页（尺寸匹配锚点页）。
// 缩略图：生成灰色占位 PNG，零重渲染。
func (s *DocumentService) InsertBlankPages(id string, atIndex, count int, before bool) (*DocInfo, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()

	if atIndex < 1 || atIndex > doc.PageCount+1 {
		return nil, NewErr("INVALID_PARAM", fmt.Sprintf("插入位置须在 1-%d 之间", doc.PageCount+1))
	}
	if count < 1 || count > 100 {
		return nil, NewErr("INVALID_PARAM", "空白页数量须在 1-100 之间")
	}
	insertOffset := atIndex - 1
	if !before {
		insertOffset = atIndex
	}

	// 锚点页尺寸：before 取目标页，after 取前一页（末尾追加取最后一页）
	anchor := atIndex
	if !before && atIndex > 1 {
		anchor = atIndex - 1
	}
	dim, err := engine.BlankPageDimFor(doc.WorkPath, anchor)
	if err != nil {
		return nil, err
	}

	out := doc.nextVersion()
	if err := engine.InsertBlankPages(doc.WorkPath, out, atIndex, count, before, dim); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	doc.WorkPath = out
	if err := doc.refresh(); err != nil {
		return nil, err
	}

	// 缩略图零重渲染：插入点右移旧文件；新页放灰色占位图
	perm := map[int]int{}
	copyFrom := map[int]string{}
	for p := 1; p <= doc.PageCount-count; p++ {
		if p < insertOffset+1 {
			perm[p] = p
		} else {
			perm[p+count] = p
		}
	}
	blankDir, err := s.store.ws.NewTempSubdir("blank-thumbs")
	if err == nil {
		defer os.RemoveAll(blankDir)
		for i := 0; i < count; i++ {
			bw, bh := 180.0, 240.0
			if i == 0 && dim != nil && dim.Width > 0 {
				bh = 180.0 * dim.Height / dim.Width
			}
			p := filepath.Join(blankDir, fmt.Sprintf("p%d.png", i+1))
			if err := writeGrayPNG(p, int(bw), int(bh)); err == nil {
				copyFrom[insertOffset+1+i] = p
			}
		}
	}
	_ = s.store.ws.RearrangeThumbs(doc.ID, doc.PageCount, perm, copyFrom)
	return doc.docInfo(), nil
}

func (s *DocumentService) DeletePages(id, pages string) (*DocInfo, error) {
	if strings.TrimSpace(pages) == "" {
		return nil, NewErr("INVALID_PARAM", "未指定要删除的页面")
	}
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()

	if err := engine.ValidatePageSelection(doc.PageCount, pages); err != nil {
		return nil, err
	}
	sel, err := engine.ParseSelection(doc.PageCount, pages)
	if err != nil {
		return nil, err
	}
	total := doc.PageCount
	out := doc.nextVersion()
	if err := engine.RemovePages(doc.WorkPath, out, pages); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	doc.WorkPath = out
	if err := doc.refresh(); err != nil {
		return nil, err
	}
	// 缩略图零重渲染：新页 k <- 未删除的第 k 个旧页
	selSet := map[int]bool{}
	for _, n := range sel {
		selSet[n] = true
	}
	perm := map[int]int{}
	k := 0
	for old := 1; old <= total; old++ {
		if !selSet[old] {
			k++
			perm[k] = old
		}
	}
	_ = s.store.ws.RearrangeThumbs(doc.ID, doc.PageCount, perm, nil)
	return doc.docInfo(), nil
}

// SaveOptions 保存时的辅助数据合并选项。
// nil 字段 = 不修改该项；空切片 = 移除该项全部内容。
type SaveOptions struct {
	Outline *[]OutlineNode `json:"outline,omitempty"`
	// PageSeq 最终页面序列（内容资源 uuid / blank-<w>x<h> 空白标记）。
	// 前端的删除/移动/插入均为纯前端操作，仅在保存/另存为时提交该序列，由后端装配。
	PageSeq *[]string    `json:"pageSeq,omitempty"`
	Labels  *[]PageLabel `json:"labels,omitempty"`
}

// SaveAs 另存为新文件：合并辅助数据后写临时文件原子替换，
// 并把会话切换到目标文件（后续 Save 写回目标；docInfo 返回新路径与文件名）。
func (s *DocumentService) SaveAs(id, targetPath string, opts *SaveOptions) (*DocInfo, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	// 与 DocStore.Create 一致，统一绝对路径
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, WrapErr("INVALID_PATH", "目标路径无效", err)
	}
	targetPath = abs

	doc.mu.Lock()
	defer doc.mu.Unlock()

	out := targetPath + ".pdfstudio-tmp"
	_ = os.Remove(out)
	if _, err := s.mergeAux(doc, opts, out); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	if err := atomicWrite(targetPath, out); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	_ = os.Remove(out)

	// 关键：无论是否有页面序列变更，都把目标文件接管为新的工作副本，
	// 并把会话的当前路径切到目标文件；此后 Save 写目标，docInfo 也返回新路径。
	if err := doc.adoptWorkCopy(targetPath); err != nil {
		return nil, err
	}
	doc.SourcePath = targetPath

	return doc.docInfo(), nil
}

// Save 保存到原始文件（合并辅助数据；临时文件 + 原子替换，避免写坏原文件）。
func (s *DocumentService) Save(id string, opts *SaveOptions) (*DocInfo, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()

	out := doc.SourcePath + ".pdfstudio-tmp"
	_ = os.Remove(out)
	applied, err := s.mergeAux(doc, opts, out)
	if err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	if err := atomicWrite(doc.SourcePath, out); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	_ = os.Remove(out)
	if applied {
		if err := doc.adoptWorkCopy(doc.SourcePath); err != nil {
			return nil, err
		}
	}
	return doc.docInfo(), nil
}

// mergeAux 工作版本 -> out：依次合并书签、页标签、附件会话（链式中间文件）。
func (s *DocumentService) mergeAux(doc *Document, opts *SaveOptions, out string) (bool, error) {
	cur := doc.WorkPath
	seqApplied := false
	// 书签/页标签的页码基准：装配后的实际页数（pageSeq 可增删页）
	effPageCount := doc.PageCount
	chain := []string{}
	defer func() {
		for _, p := range chain {
			_ = os.Remove(p)
		}
	}()
	nextOut := func(tag string) string {
		p := fmt.Sprintf("%s.%s", out, tag)
		chain = append(chain, p)
		return p
	}

	// 0) 页面序列装配：删除/移动/插入空白页等纯前端编辑在此落地
	if opts != nil && opts.PageSeq != nil {
		seq := *opts.PageSeq
		if len(seq) == 0 {
			return seqApplied, NewErr("INVALID_PARAM", "页面序列为空")
		}
		if !equalSeq(seq, doc.res) {
			pieces, err := s.piecesFor(seq)
			if err != nil {
				return seqApplied, err
			}
			merged := nextOut("seq")
			if err := engine.AssemblePages(pieces, merged); err != nil {
				return seqApplied, err
			}
			cur = merged
			seqApplied = true
			effPageCount = len(seq)
		}
		doc.res = append([]string(nil), seq...)
	}

	// 1) 书签：replace（空切片 = 清空全部书签）
	if opts != nil && opts.Outline != nil {
		mid := nextOut("bm")
		if len(*opts.Outline) == 0 {
			if err := engine.RemoveBookmarks(cur, mid); err != nil {
				return seqApplied, err
			}
		} else if err := engine.WriteBookmarks(cur, mid, toEngineBookmarks(clampOutlinePages(*opts.Outline, effPageCount)), true); err != nil {
			return seqApplied, err
		}
		cur = mid
	}

	// 2) 页标签：replace（空切片 = 移除全部页标签）
	if opts != nil && opts.Labels != nil {
		mid := nextOut("pl")
		if len(*opts.Labels) == 0 {
			if err := engine.RemovePageLabels(cur, mid); err != nil {
				return seqApplied, err
			}
		} else if err := writeLabelsFile(cur, mid, *opts.Labels); err != nil {
			return seqApplied, err
		}
		cur = mid
	}

	// 3) 附件会话：移除 + 新增
	if doc.att != nil && doc.att.hasChanges() {
		mid := nextOut("att")
		if err := doc.att.apply(cur, mid); err != nil {
			return seqApplied, err
		}
		cur = mid
	}

	if cur == doc.WorkPath {
		// 无辅助数据变更：直接落一份工作版本副本，保证 out 始终存在
		return seqApplied, copyFile(cur, out)
	}
	return seqApplied, os.Rename(cur, out)
}

// atomicWrite 将 src 内容以临时文件 + rename 的方式写入 target，保证原子性。
func atomicWrite(target, src string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return WrapErr("SAVE_FAILED", "目标目录不可写", err)
	}
	tmp, err := os.CreateTemp(dir, ".pdfstudio-save-*")
	if err != nil {
		return WrapErr("SAVE_FAILED", "创建临时文件失败", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Close(); err != nil {
		return WrapErr("SAVE_FAILED", "写入临时文件失败", err)
	}
	// 复制而非 rename 工作版本：保留工作版本供继续编辑
	if err := copyFile(src, tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return WrapErr("SAVE_FAILED", "替换目标文件失败", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return WrapErr("SAVE_FAILED", "读取工作版本失败", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return WrapErr("SAVE_FAILED", "写入目标失败", err)
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return WrapErr("SAVE_FAILED", "复制文件内容失败", err)
	}
	return nil
}

// randomToken 生成短随机串用于任务 ID。
func randomToken() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PageThumbnail 同步渲染第 pageNr 页（1-based）缩略图，返回可访问 URL。
// 宽度上限 600px；已存在同名缓存时直接返回。用于封面与其他面板的页面预览。
// InsertImagePage 把一张图片作为一页插入文档：页面尺寸继承锚点页，
// 图片等比缩放完整放入。atIndex 1-based；before=true 插在 atIndex 之前。
// MovePages 将选中页按原顺序移动到 atIndex 位置（1-based，基于移动前编号）。
// before=true 表示放在原第 atIndex 页之前，false 为之后。
func (s *DocumentService) MovePages(id, pages string, atIndex int, before bool) (*DocInfo, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()

	if err := engine.ValidatePageSelection(doc.PageCount, pages); err != nil {
		return nil, err
	}
	sel, err := engine.ParseSelection(doc.PageCount, pages)
	if err != nil {
		return nil, err
	}
	total := doc.PageCount
	out := doc.nextVersion()
	if err := engine.MovePages(doc.WorkPath, pages, atIndex, before, out); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	doc.WorkPath = out
	if err := doc.refresh(); err != nil {
		return nil, err
	}
	// 缩略图零重渲染：计算新序 -> 旧页映射并重排文件
	selSet := map[int]bool{}
	for _, n := range sel {
		selSet[n] = true
	}
	rest := []int{}
	for i := 1; i <= total; i++ {
		if !selSet[i] {
			rest = append(rest, i)
		}
	}
	// 插入点在“剩余页”序列中的下标：before=第一个 >= atIndex 的位置；after=其后
	ins := len(rest)
	for i, p := range rest {
		if (before && p >= atIndex) || (!before && p > atIndex) {
			ins = i
			break
		}
	}
	newList := append([]int{}, rest[:ins]...)
	newList = append(newList, sel...)
	newList = append(newList, rest[ins:]...)
	perm := map[int]int{}
	for k, old := range newList {
		perm[k+1] = old
	}
	_ = s.store.ws.RearrangeThumbs(doc.ID, total, perm, nil)
	return doc.docInfo(), nil
}

// ImportRequest 批量导入请求：混合 PDF 与图片，按给定顺序合成待放置内容。
type ImportRequest struct {
	Paths []string `json:"paths"`
}

// Import 批量导入 PDF/图片到挂起缓冲区（异步任务）。
// 完成事件 result 为 {bufferID, pageCount}；缓冲区缩略图位于 /imgs/<bufferID>/pN.png。
// 同一文档同时只保留一个缓冲区，新导入会覆盖旧缓冲区。
func (s *DocumentService) Import(id string, req ImportRequest) (string, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	paths := append([]string(nil), req.Paths...)
	if len(paths) == 0 {
		return "", NewErr("INVALID_PARAM", "请先选择要导入的文件")
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return "", NewErr("FILE_NOT_FOUND", fmt.Sprintf("文件不存在: %s", filepath.Base(p)))
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".pdf" && !IsImageFile(p) {
			return "", NewErr("INVALID_PARAM", fmt.Sprintf("不支持的文件类型: %s", filepath.Base(p)))
		}
	}

	taskID := "import-" + randomToken()
	s.bus.Task(taskID, "导入文件", func(report func(current, total int, msg string)) (any, error) {
		bufID := "img-" + randomToken()
		bufDir := s.store.ws.ImagesDir(bufID)
		if err := os.MkdirAll(bufDir, 0o755); err != nil {
			return nil, WrapErr("WORKSPACE_CREATE", "创建导入目录失败", err)
		}
		bufPDF := filepath.Join(bufDir, "buffer.pdf")

		// pieceInfo 记录一个片段在缓冲区内的角色：
		// path 是实际参与合并的文件；src 仅当片段来自用户选择的 PDF 时非空，
		// 用于后续填充 ImportBuffer.Sources（页标签/附件合并的来源）。
		type pieceInfo struct {
			path string
			src  string
		}

		var infos []pieceInfo
		cleanup := []string{}
		defer func() {
			for _, f := range cleanup {
				_ = os.Remove(f)
			}
		}()

		total := len(paths)
		// 按用户选择的原始顺序扫描：PDF 各自成段，连续图片合并为一个段，
		// 且该图片段必须保持在原位置，不得整体移到末尾。
		for i := 0; i < len(paths); {
			p := paths[i]
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".pdf" {
				report(i, total, fmt.Sprintf("解析 %d/%d：%s", i+1, total, filepath.Base(p)))
				infos = append(infos, pieceInfo{path: p, src: p})
				i++
				continue
			}
			// 连续图片：从 i 开始收集，遇到 PDF 停下
			start := i
			var images []string
			for i < len(paths) {
				q := paths[i]
				qe := strings.ToLower(filepath.Ext(q))
				if qe == ".pdf" {
					break
				}
				report(i, total, fmt.Sprintf("解析 %d/%d：%s", i+1, total, filepath.Base(q)))
				images = append(images, q)
				i++
			}
			tmpImg := filepath.Join(bufDir, fmt.Sprintf("images-part-%d.pdf", start))
			if err := engine.ImportImagesToPDF(images, tmpImg, engine.ImageImportConfig{
				PageSize: engine.PageSizeAuto,
			}); err != nil {
				_ = os.RemoveAll(bufDir)
				return nil, WrapErr("IMPORT_FAILED", "图片解析失败", err)
			}
			cleanup = append(cleanup, tmpImg)
			infos = append(infos, pieceInfo{path: tmpImg})
		}

		pieces := make([]string, 0, len(infos))
		for _, info := range infos {
			pieces = append(pieces, info.path)
		}

		report(total, total, "生成待放置内容")
		if len(pieces) == 1 {
			if err := copyFile(pieces[0], bufPDF); err != nil {
				_ = os.RemoveAll(bufDir)
				return nil, err
			}
		} else {
			if err := engine.MergeFiles(pieces, bufPDF); err != nil {
				_ = os.RemoveAll(bufDir)
				return nil, WrapErr("IMPORT_FAILED", "导入内容合成失败", err)
			}
		}

		count, err := engine.PageCount(bufPDF)
		if err != nil {
			_ = os.RemoveAll(bufDir)
			return nil, WrapErr("IMPORT_FAILED", "导入内容无法解析", err)
		}

		// 渲染缓冲区缩略图（同步，保证放置前可预览）
		if _, err := engine.RenderThumbnailsDir(bufPDF, bufDir, 160, 1, func(done, t int) {
			report(done, t, fmt.Sprintf("渲染预览 %d/%d", done, t))
		}); err != nil && count > 0 {
			// 预览失败不阻塞，页面仍可插入
			_ = count
		}

		// 计算每个 PDF 片段在缓冲区内的起始页，填充 Sources。
		// 图片片段不是用户原始 PDF，不进入 Sources（无页标签/附件可合并）。
		var sources []BufSource
		start := 0
		for _, info := range infos {
			cnt, cerr := engine.PageCount(info.path)
			if cerr != nil {
				cnt = 0
			}
			if info.src != "" {
				sources = append(sources, BufSource{
					Path:  info.src,
					Start: start,
					Count: cnt,
				})
			}
			start += cnt
		}

		doc.mu.Lock()
		old := doc.buffer
		doc.buffer = &ImportBuffer{
			ID:        bufID,
			PDFPath:   bufPDF,
			ThumbsDir: bufDir,
			PageCount: count,
			Sources:   sources,
		}
		doc.mu.Unlock()
		if old != nil {
			_ = os.RemoveAll(old.ThumbsDir)
		}

		// 内容资源：为每个缓冲页渲染基图并注册 uuid（bufPDF 持久保留，保存装配时取页）
		res := make([]string, 0, count)
		for p := 1; p <= count; p++ {
			u, uerr := s.ensurePageRes(bufPDF, p)
			if uerr != nil {
				return nil, uerr
			}
			res = append(res, u)
		}
		dims, _ := engine.PageDims(bufPDF)
		pds := make([]PageDim, 0, len(dims))
		for _, d := range dims {
			pds = append(pds, PageDim{Width: d.Width, Height: d.Height})
		}
		return map[string]any{"bufferID": bufID, "pageCount": count, "res": res, "dims": pds}, nil
	})
	return taskID, nil
}

// PlaceResult 放置结果：文档信息 + 合并进当前会话的辅助数据（前端据此同步各子页面）。
type PlaceResult struct {
	Info             *DocInfo         `json:"info"`
	AddedOutline     []OutlineNode    `json:"addedOutline,omitempty"`
	AddedLabels      []PageLabel      `json:"addedLabels,omitempty"`
	AddedAttachments []AttachmentInfo `json:"addedAttachments,omitempty"`
}

// AbsorbBufferAux 只合并缓冲区的辅助数据（书签/页标签/附件，页码平移），不改页面结构。
// 纯前端页面流：页面顺序由前端 pageSeq 维护，仅在保存时装配。
func (s *DocumentService) AbsorbBufferAux(id string, atIndex int, before bool) (*PlaceResult, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()
	if doc.buffer == nil {
		return &PlaceResult{Info: doc.docInfo()}, nil
	}
	insertOffset := atIndex - 1
	if !before {
		insertOffset = atIndex
	}
	if insertOffset < 0 {
		insertOffset = 0
	}
	buf := doc.buffer
	doc.buffer = nil
	return s.absorbAuxLocked(doc, buf, insertOffset), nil
}

// absorbAuxLocked 读取缓冲区来源 PDF 的书签/页标签/附件并平移合并进文档会话。
func (s *DocumentService) absorbAuxLocked(doc *Document, buf *ImportBuffer, insertOffset int) *PlaceResult {
	res := &PlaceResult{Info: doc.docInfo()}
	// 1) 书签：缓冲区 PDF 已合并各来源书签（页码相对缓冲区），整体平移
	if bms, berr := engine.ReadBookmarks(buf.PDFPath); berr == nil && len(bms) > 0 {
		nodes := fromEngineBookmarks(bms)
		shiftOutline(nodes, insertOffset)
		res.AddedOutline = nodes
	}
	// 2) 页标签 + 附件：按来源 PDF 读取并平移
	attachDir := s.store.ws.AttachDir(doc.ID)
	for _, src := range buf.Sources {
		if spec, lerr := engine.ReadPageLabels(src.Path); lerr == nil && spec != nil {
			for _, r := range spec.Ranges {
				res.AddedLabels = append(res.AddedLabels, PageLabel{
					StartPage:  r.StartPage + src.Start + insertOffset,
					Prefix:     r.Prefix,
					Style:      string(r.Style),
					StartValue: r.StartValue,
				})
			}
		}
		if aa, aerr := engine.ListAttachments(src.Path); aerr == nil {
			for _, a := range aa {
				if a.FileName == "" {
					continue
				}
				f, ferr := engine.ExtractAttachment(src.Path, a.FileName, attachDir)
				if ferr != nil {
					continue
				}
				doc.att.add(StoredAttachment{Name: a.FileName, Desc: a.Desc, FilePath: f, ModTime: a.ModTime})
				res.AddedAttachments = append(res.AddedAttachments, AttachmentInfo{
					FileName: a.FileName, Description: a.Desc, ModTime: a.ModTime,
				})
			}
		}
	}
	return res
}

// PlaceBuffer 把挂起的导入内容整体插入到 atIndex 位置（1-based），
// 并把导入 PDF 的书签/页标签/附件合并进当前会话（页码自动平移）。
func (s *DocumentService) PlaceBuffer(id string, atIndex int, before bool) (*PlaceResult, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()

	if doc.buffer == nil {
		return nil, NewErr("INVALID_PARAM", "没有待放置的导入内容")
	}
	if atIndex < 1 || atIndex > doc.PageCount+1 {
		return nil, NewErr("INVALID_PARAM", fmt.Sprintf("插入位置须在 1-%d 之间", doc.PageCount+1))
	}
	insertOffset := atIndex - 1 // 0-based：before 放在原第 atIndex 页前
	if !before {
		insertOffset = atIndex
	}

	buf := doc.buffer
	oldCount := doc.PageCount
	bufCount := buf.PageCount
	out := doc.nextVersion()
	if err := engine.InsertFromPDF(doc.WorkPath, atIndex, buf.PDFPath, "*", out); err != nil {
		_ = os.Remove(out)
		return nil, err
	}
	doc.WorkPath = out
	doc.buffer = nil
	if err := doc.refresh(); err != nil {
		return nil, err
	}

	// 缩略图零重渲染：插入点前原位保留；缓冲区预览图复制到位；其余右移
	perm := map[int]int{}
	copyFrom := map[int]string{}
	for p := 1; p <= oldCount; p++ {
		if p < insertOffset+1 {
			perm[p] = p
		} else {
			perm[p+bufCount] = p
		}
	}
	for i := 0; i < bufCount; i++ {
		copyFrom[insertOffset+1+i] = filepath.Join(buf.ThumbsDir, fmt.Sprintf("p%d.png", i+1))
	}
	_ = s.store.ws.RearrangeThumbs(doc.ID, doc.PageCount, perm, copyFrom)
	defer func() { _ = os.RemoveAll(buf.ThumbsDir) }()

	res := s.absorbAuxLocked(doc, buf, insertOffset)
	return res, nil
}

// shiftOutline 递归平移书签目标页（1-based）。
func shiftOutline(nodes []OutlineNode, off int) {
	for i := range nodes {
		nodes[i].Page += off
		shiftOutline(nodes[i].Kids, off)
	}
}

// DiscardBuffer 丢弃挂起的导入内容。
func (s *DocumentService) DiscardBuffer(id string) error {
	doc, err := s.store.Get(id)
	if err != nil {
		return err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()
	if doc.buffer != nil {
		_ = os.RemoveAll(doc.buffer.ThumbsDir)
		doc.buffer = nil
	}
	return nil
}

// writeGrayPNG 生成纯灰占位图（空白页缩略图）。
func writeGrayPNG(path string, w, h int) error {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 0xED, G: 0xED, B: 0xEE, A: 0xFF}}, image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
