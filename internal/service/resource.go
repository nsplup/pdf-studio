package service

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfstudio/internal/engine"
)

// 页面资源 uuid：随机句柄，唯一对应一个来源页。
// 不再用缩略图哈希做内容去重。
var resourceIDRe = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func newResourceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	// RFC 4122 UUID v4
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}

// IsResourceID 判断目录段是否为页面资源 uuid。
func IsResourceID(s string) bool { return resourceIDRe.MatchString(s) }

// ParseBlankMarker 解析空白页标记 blank-<宽pt>x<高pt>。
func ParseBlankMarker(s string) (w, h float64, ok bool) {
	if !strings.HasPrefix(s, "blank-") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(s, "blank-"), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	var ww, hh float64
	if _, err := fmt.Sscanf(parts[0], "%f", &ww); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(parts[1], "%f", &hh); err != nil {
		return 0, 0, false
	}
	if ww <= 0 || hh <= 0 {
		return 0, 0, false
	}
	return ww, hh, true
}

// pageSource 记录资源 uuid 的内容来源（持久化的 PDF 文件 + 页码）。
type pageSource struct {
	Path string
	Page int
}

// allocatePageRes 分配资源 uuid 并登记来源；不渲染任何像素。
// 返回 uuid 与该资源的目录路径，渲染由调用方完成。
func (s *DocumentService) allocatePageRes(workPath string, page int) (string, string, error) {
	var u, resDir string
	for i := 0; i < 20; i++ {
		id, err := newResourceID()
		if err != nil {
			return "", "", err
		}
		if _, loaded := s.store.resReg.Load(id); loaded {
			continue
		}
		dir := s.store.ws.ResourceDir(id)
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", "", err
		}
		// 用 Mkdir 而不是 MkdirAll：目录已存在说明碰撞，换一个
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		u, resDir = id, dir
		break
	}
	if u == "" {
		return "", "", NewErr("RESOURCE_ID_FAILED", "生成页面资源标识失败")
	}
	s.store.resReg.Store(u, pageSource{Path: workPath, Page: page})
	return u, resDir, nil
}

// ensurePageRes 分配资源并立即渲染 140px 基图（单页场景，供 HealResource 等使用）。
func (s *DocumentService) ensurePageRes(workPath string, page int) (string, error) {
	u, dir, err := s.allocatePageRes(workPath, page)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "140.png")
	if err := engine.RenderPagePNGToFile(workPath, page, 140, dst); err != nil {
		_ = os.RemoveAll(dir)
		return "", WrapErr("RENDER_FAILED", fmt.Sprintf("渲染第 %d 页失败", page), err)
	}
	return u, nil
}

// ensurePageResBatch 一次打开文档，批量分配并渲染 1..count 页的 140px 基图。
// onPage 每完成一页回调（done 从 1 开始）；为 nil 时静默。
// 返回的 uuid 列表按页码顺序排列。
//
// 任一页失败时回滚所有已分配的资源目录，并返回错误。
func (s *DocumentService) ensurePageResBatch(
	workPath string, count int,
	onPage func(done, total int),
) ([]string, error) {
	res := make([]string, 0, count)
	dirs := make([]string, 0, count)
	pages := make([]int, 0, count)
	outs := make([]string, 0, count)

	// 阶段 1：分配 uuid 与目录（轻量）
	for p := 1; p <= count; p++ {
		u, dir, err := s.allocatePageRes(workPath, p)
		if err != nil {
			for _, d := range dirs {
				_ = os.RemoveAll(d)
			}
			return nil, err
		}
		res = append(res, u)
		dirs = append(dirs, dir)
		pages = append(pages, p)
		outs = append(outs, filepath.Join(dir, "140.png"))
	}

	// 阶段 2：批量渲染（打开一次文档）
	if err := engine.RenderPagesToFiles(workPath, pages, outs, 140, onPage); err != nil {
		for _, d := range dirs {
			_ = os.RemoveAll(d)
		}
		return nil, WrapErr("RENDER_FAILED", "批量渲染页面资源失败", err)
	}
	return res, nil
}

// PageResources 异步渲染文档全部页面的资源基图；返回任务 ID。
// 完成事件 result 为资源 uuid 有序列表。
// 已缓存（doc.res 与 PageCount 一致）时任务立即完成。
func (s *DocumentService) PageResources(id string) (string, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	taskID := "res-" + id
	s.bus.Task(taskID, "渲染页面资源", func(report func(Progress)) (any, error) {
		doc.mu.Lock()
		defer doc.mu.Unlock()

		// 缓存命中：直接返回
		if len(doc.res) == doc.PageCount && doc.PageCount > 0 {
			out := make([]string, len(doc.res))
			copy(out, doc.res)
			return out, nil
		}
		if doc.PageCount <= 0 {
			return []string{}, nil
		}

		res, err := s.ensurePageResBatch(doc.WorkPath, doc.PageCount,
			func(done, total int) {
				report(Progress{
					Current: done,
					Total:   total,
					Phase:   "渲染页面资源",
				})
			})
		if err != nil {
			return nil, err
		}
		doc.res = res
		out := make([]string, len(res))
		copy(out, res)
		return out, nil
	})
	return taskID, nil
}

// piecesFor 把 uuid 序列解析为装配单元（来源页或空白页）。
func (s *DocumentService) piecesFor(seq []string) ([]engine.Piece, error) {
	pieces := make([]engine.Piece, 0, len(seq))
	for _, u := range seq {
		if w, h, ok := ParseBlankMarker(u); ok {
			pieces = append(pieces, engine.Piece{BlankDim: &types.Dim{Width: w, Height: h}})
			continue
		}
		v, ok := s.store.resReg.Load(u)
		if !ok {
			return nil, NewErr("RESOURCE_MISSING", "页面资源缺失或已过期，请重新打开文档后重试")
		}
		ps := v.(pageSource)
		pieces = append(pieces, engine.Piece{Path: ps.Path, Page: ps.Page})
	}
	return pieces, nil
}

// HealResource 渲染资源 uuid 的 <width>px 图（静态服务缺失时自愈）。
func (s *DocumentService) HealResource(uuid string, width int) error {
	if !IsResourceID(uuid) || width < 16 || width > 4000 {
		return NewErr("INVALID_PARAM", "非法资源请求")
	}
	v, ok := s.store.resReg.Load(uuid)
	if !ok {
		return NewErr("RESOURCE_MISSING", "页面资源未注册")
	}
	ps := v.(pageSource)
	resDir := s.store.ws.ResourceDir(uuid)
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(resDir, fmt.Sprintf("%d.png", width))
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	tmp := dst + ".tmp"
	if err := engine.RenderPagePNGToFile(ps.Path, ps.Page, width, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

// equalSeq 判断两个页面序列是否一致。
func equalSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
