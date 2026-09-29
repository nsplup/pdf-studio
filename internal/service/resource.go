package service

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"pdfstudio/internal/engine"
)

// 页面资源模型：每页内容对应一个内容哈希（sha1 hex）uuid，
// 资源文件位于 <ws>/thumbs/<uuid>/<宽>.png，可跨文档、跨会话复用。
// 前端持有 uuid 有序列表（页面逻辑顺序），删除/移动/插入均为纯前端操作，
// 仅在保存/另存为时把最终序列交给后端装配。

var resourceIDRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

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

func hashFileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pageSource 记录资源 uuid 的内容来源（持久化的 PDF 文件 + 页码）。
type pageSource struct {
	Path string
	Page int
}

// ensurePageRes 渲染文档第 page 页 140px 基图并注册内容资源，返回 uuid。
func (s *DocumentService) ensurePageRes(workPath string, page int) (string, error) {
	tmp := engine.TempPath("res-hash.png")
	defer func() { _ = os.Remove(tmp) }()
	if err := engine.RenderPagePNGToFile(workPath, page, 140, tmp); err != nil {
		return "", WrapErr("RENDER_FAILED", fmt.Sprintf("渲染第 %d 页失败", page), err)
	}
	u, err := hashFileSum(tmp)
	if err != nil {
		return "", err
	}
	resDir := s.store.ws.ResourceDir(u)
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(resDir, "140.png")
	if _, err := os.Stat(dst); err != nil {
		_ = os.Rename(tmp, dst)
	}
	s.store.resReg.Store(u, pageSource{Path: workPath, Page: page})
	return u, nil
}

// PageResources 返回文档全部页面的资源 uuid 序列（首次调用会渲染全部页面的基图）。
// 同时刷新 doc.res：该序列即页面逻辑顺序，前端据此拼装缩略图/预览地址。
func (s *DocumentService) PageResources(id string) ([]string, error) {
	doc, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	doc.mu.Lock()
	defer doc.mu.Unlock()
	if len(doc.res) == doc.PageCount && doc.PageCount > 0 {
		out := make([]string, len(doc.res))
		copy(out, doc.res)
		return out, nil
	}
	res := make([]string, 0, doc.PageCount)
	for p := 1; p <= doc.PageCount; p++ {
		u, err := s.ensurePageRes(doc.WorkPath, p)
		if err != nil {
			return nil, err
		}
		res = append(res, u)
	}
	doc.res = res
	out := make([]string, len(res))
	copy(out, res)
	return out, nil
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
