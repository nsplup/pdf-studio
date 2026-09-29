package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"

	"pdfstudio/internal/engine"
)

// OutlineService 书签/目录读取、提取、修改。
type OutlineService struct {
	bus *EventBus
}

func NewOutlineService(bus *EventBus) *OutlineService {
	return &OutlineService{bus: bus}
}

// OutlineNode 书签树节点。
type OutlineNode struct {
	Title  string        `json:"title"`
	Page   int           `json:"page"` // 1-based 目标页
	Bold   bool          `json:"bold,omitempty"`
	Italic bool          `json:"italic,omitempty"`
	Kids   []OutlineNode `json:"kids,omitempty"`
}

// Read 读取文件的书签树。
func (s *OutlineService) Read(path string) ([]OutlineNode, error) {
	if err := checkPDFFile(path); err != nil {
		return nil, err
	}
	bms, err := engine.ReadBookmarks(path)
	if err != nil {
		return nil, err
	}
	return fromEngineBookmarks(bms), nil
}

// ExportJSON 提取目录结构为 pdfcpu JSON 文件。
func (s *OutlineService) ExportJSON(inPath, outPath string) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	return engine.ExportBookmarksJSON(inPath, outPath)
}

// ApplyInPlace 将编辑后的书签树写回：输出到新文件（不覆盖原文件由前端/用户决定路径）。
func (s *OutlineService) Apply(inPath, outPath string, nodes []OutlineNode, replace bool) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	if filepath.Clean(inPath) == filepath.Clean(outPath) {
		return NewErr("OVERWRITE_FORBIDDEN", "为防止数据损坏，输出文件不能与原文件相同")
	}
	bms := toEngineBookmarks(nodes)
	if len(bms) == 0 {
		return engine.RemoveBookmarks(inPath, outPath)
	}
	return engine.WriteBookmarks(inPath, outPath, bms, replace)
}

// RemoveAllToFile 移除全部书签并写入 outPath。
func (s *OutlineService) RemoveAllToFile(inPath, outPath string) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	if filepath.Clean(inPath) == filepath.Clean(outPath) {
		return NewErr("OVERWRITE_FORBIDDEN", "为防止数据损坏，输出文件不能与原文件相同")
	}
	return engine.RemoveBookmarks(inPath, outPath)
}

// ImportJSON 从 pdfcpu JSON 导入书签。
func (s *OutlineService) ImportJSON(inPath, jsonPath, outPath string, replace bool) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	return engine.ImportBookmarksJSON(inPath, jsonPath, outPath, replace)
}

// ---------- 转换 ----------

func fromEngineBookmarks(bms []pdfcpu.Bookmark) []OutlineNode {
	nodes := make([]OutlineNode, 0, len(bms))
	for _, b := range bms {
		nodes = append(nodes, OutlineNode{
			Title:  b.Title,
			Page:   b.PageFrom,
			Bold:   b.Bold,
			Italic: b.Italic,
			Kids:   fromEngineBookmarks(b.Kids),
		})
	}
	return nodes
}

// clampOutlinePages 递归把书签目标页钳制到 [1, pageCount]，避免越页书签写入失败。
func clampOutlinePages(nodes []OutlineNode, pageCount int) []OutlineNode {
	for i := range nodes {
		if nodes[i].Page < 1 {
			nodes[i].Page = 1
		}
		if pageCount > 0 && nodes[i].Page > pageCount {
			nodes[i].Page = pageCount
		}
		nodes[i].Kids = clampOutlinePages(nodes[i].Kids, pageCount)
	}
	return nodes
}

func toEngineBookmarks(nodes []OutlineNode) []pdfcpu.Bookmark {
	bms := make([]pdfcpu.Bookmark, 0, len(nodes))
	for _, n := range nodes {
		bms = append(bms, pdfcpu.Bookmark{
			Title:    n.Title,
			PageFrom: n.Page,
			Bold:     n.Bold,
			Italic:   n.Italic,
			Kids:     toEngineBookmarks(n.Kids),
		})
	}
	return bms
}

// checkPDFFile 基础校验：存在且可读。
func checkPDFFile(path string) error {
	if path == "" {
		return NewErr("INVALID_PARAM", "未指定文件")
	}
	st, err := os.Stat(path)
	if err != nil {
		return NewErr("FILE_NOT_FOUND", fmt.Sprintf("文件不存在: %s", filepath.Base(path)))
	}
	if st.IsDir() {
		return NewErr("INVALID_PARAM", "路径是目录而不是文件")
	}
	return nil
}

// OutlineTextItem 纯文本目录行：Depth 为前导制表符数量，Page 为目标页。
type OutlineTextItem struct {
	Depth int    `json:"depth"`
	Title string `json:"title"`
	Page  int    `json:"page"`
}

// ExportText 导出书签为人类可读的缩进文本（每行一个节点：
// 前导 \t 表示层级；标题与页码以 \t 分隔），写入 outPath。
func (s *OutlineService) ExportText(inPath, outPath string) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	bms, err := engine.ReadBookmarks(inPath)
	if err != nil {
		return err
	}
	var sb strings.Builder
	var walk func(nodes []pdfcpu.Bookmark, depth int)
	walk = func(nodes []pdfcpu.Bookmark, depth int) {
		for _, b := range nodes {
			sb.WriteString(strings.Repeat("\t", depth))
			sb.WriteString(b.Title)
			sb.WriteString("\t")
			sb.WriteString(strconv.Itoa(b.PageFrom))
			sb.WriteString("\n")
			walk(b.Kids, depth+1)
		}
	}
	walk(bms, 0)
	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		return WrapErr("EXPORT_FAILED", "写出目录文本失败", err)
	}
	return nil
}
