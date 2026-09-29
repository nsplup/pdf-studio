package service

import (
	"net/url"

	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pdfstudio/internal/engine"
)

// ImageService 多图片合并为 PDF。
type ImageService struct {
	bus *EventBus
	ws  *Workspace
}

func NewImageService(bus *EventBus, ws *Workspace) *ImageService {
	return &ImageService{bus: bus, ws: ws}
}

// ImagesToPDFRequest 图片导入请求。
type ImagesToPDFRequest struct {
	ImagePaths []string `json:"imagePaths"`
	OutputPath string   `json:"outputPath"`
	PageSize   string   `json:"pageSize"` // A4 / A3 / Letter / Auto
	Landscape  bool     `json:"landscape"`
	MarginPt   float64  `json:"marginPt"`
	SortMode   string   `json:"sortMode"` // given | natural
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".tif": true, ".tiff": true, ".bmp": true, ".webp": true,
}

// IsImageFile 判断扩展名是否为支持的图片类型。
func IsImageFile(path string) bool {
	return imageExts[strings.ToLower(filepath.Ext(path))]
}

// Start 异步执行图片转 PDF；完成事件 result 为输出文件路径。
func (s *ImageService) Start(req ImagesToPDFRequest) (string, error) {
	if len(req.ImagePaths) < 1 {
		return "", NewErr("INVALID_PARAM", "请先添加图片")
	}
	if req.OutputPath == "" {
		return "", NewErr("INVALID_PARAM", "未指定输出文件")
	}
	images := append([]string(nil), req.ImagePaths...)
	for _, p := range images {
		if !IsImageFile(p) {
			return "", NewErr("INVALID_PARAM", fmt.Sprintf("不支持的图片格式: %s", filepath.Base(p)))
		}
		if _, err := os.Stat(p); err != nil {
			return "", NewErr("FILE_NOT_FOUND", fmt.Sprintf("图片不存在: %s", filepath.Base(p)))
		}
	}
	if req.SortMode == "natural" {
		NaturalSortStrings(images)
	}
	// "given" 模式保持用户在面板中的排列顺序
	if req.PageSize == "" {
		req.PageSize = "A4"
	}
	if req.MarginPt < 0 {
		req.MarginPt = 0
	}

	taskID := "img2pdf-" + randomToken()
	s.bus.Task(taskID, "图片转 PDF", func(report func(current, total int, msg string)) (any, error) {
		total := len(images)
		for i := range images {
			report(i+1, total, fmt.Sprintf("校验图片 %d/%d", i+1, total))
		}
		report(total, total, "生成 PDF")
		cfg := engine.ImageImportConfig{
			PageSize:  engine.PageSizeMode(req.PageSize),
			Landscape: req.Landscape,
			MarginPt:  req.MarginPt,
		}
		if err := engine.ImportImagesToPDF(images, req.OutputPath, cfg); err != nil {
			return nil, err
		}
		return req.OutputPath, nil
	})
	return taskID, nil
}

// ImageItem 图片会话中的单个条目；URL 供前端 <img> 预览。
type ImageItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Prepare 把选中的图片复制进工作区会话目录并返回可预览的条目列表。
// 顺序即列表顺序；前端可自行调整顺序后作为 Start 的 ImagePaths。
func (s *ImageService) Prepare(imagePaths []string) ([]ImageItem, error) {
	if len(imagePaths) == 0 {
		return nil, NewErr("INVALID_PARAM", "请先添加图片")
	}
	sessID := fmt.Sprintf("img-%s", randomToken())
	dir := s.ws.ImagesDir(sessID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, WrapErr("WORKSPACE_CREATE", "创建图片会话失败", err)
	}
	items := make([]ImageItem, 0, len(imagePaths))
	for _, p := range imagePaths {
		if !IsImageFile(p) {
			return nil, NewErr("INVALID_PARAM", fmt.Sprintf("不支持的图片格式: %s", filepath.Base(p)))
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return nil, NewErr("FILE_NOT_FOUND", fmt.Sprintf("图片不存在: %s", filepath.Base(p)))
		}
		base := filepath.Base(p)
		dst := filepath.Join(dir, base)
		// 同名去重：追加序号
		for i := 2; ; i++ {
			if _, err := os.Stat(dst); err != nil {
				break
			}
			ext := filepath.Ext(base)
			dst = filepath.Join(dir, fmt.Sprintf("%s_%d%s", strings.TrimSuffix(base, ext), i, ext))
		}
		if err := copyFile(p, dst); err != nil {
			return nil, WrapErr("SAVE_FAILED", fmt.Sprintf("复制图片失败: %s", base), err)
		}
		items = append(items, ImageItem{
			Path: p, // 转换仍用原文件路径，避免重复占用空间
			Name: base,
			URL:  "/imgs/" + sessID + "/" + url.PathEscape(filepath.Base(dst)),
		})
	}
	return items, nil
}
