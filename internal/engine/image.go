package engine

import (
	"fmt"
	"image"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"math"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PageSizeMode 图片转 PDF 的页面尺寸策略。
type PageSizeMode string

const (
	PageSizeA4     PageSizeMode = "A4"
	PageSizeA3     PageSizeMode = "A3"
	PageSizeLetter PageSizeMode = "Letter"
	// PageSizeAuto 每页尺寸等于图片尺寸（72dpi 下的像素尺寸）
	PageSizeAuto PageSizeMode = "Auto"
)

// ImageImportConfig 图片导入参数。
type ImageImportConfig struct {
	PageSize  PageSizeMode `json:"pageSize"`  // A4/A3/Letter/Auto
	Landscape bool         `json:"landscape"` // 横向
	MarginPt  float64      `json:"marginPt"`  // 四边留白（pt），Auto 模式忽略
	Pos       string       `json:"pos"`       // 锚点：full 居中铺满余量区域
}

// ImportImagesToPDF 将图片列表按顺序合成为 PDF。
// PageSizeAuto 模式下每张图按自身像素尺寸出一页；其他模式整批共用统一纸张。
func ImportImagesToPDF(imgFiles []string, outPath string, cfg ImageImportConfig) error {
	if len(imgFiles) == 0 {
		return fmt.Errorf("没有可导入的图片")
	}
	if cfg.PageSize == PageSizeAuto {
		return importImagesAutoPerPage(imgFiles, outPath, cfg)
	}
	imp, err := buildImportConfig(cfg, imgFiles[0])
	if err != nil {
		return err
	}
	if err := api.ImportImagesFile(imgFiles, outPath, imp, Config()); err != nil {
		return fmt.Errorf("图片转 PDF 失败: %w", err)
	}
	return nil
}

// importImagesAutoPerPage 逐张按自身像素尺寸生成单页 PDF，最后合并为一份。
// pdfcpu 的 ImportImagesFile 整批共用一份 PageDim，无法做到"每张图不同尺寸"，
// 只能逐张生成子 PDF 再合并。
func importImagesAutoPerPage(imgFiles []string, outPath string, cfg ImageImportConfig) error {
	tmp := &TempNames{}
	defer tmp.Cleanup()

	parts := make([]string, 0, len(imgFiles))
	for i, img := range imgFiles {
		dim, err := imageFileDim(img)
		if err != nil {
			return fmt.Errorf("读取图片尺寸失败 (%s): %w", filepath.Base(img), err)
		}
		part := tmp.New(fmt.Sprintf("auto-%d.pdf", i))
		imp := pdfcpu.DefaultImportConfig()
		imp.InpUnit = types.POINTS
		imp.Pos = types.Center
		imp.UserDim = true
		imp.PageDim = &types.Dim{
			Width:  dim.Width + 2*cfg.MarginPt,
			Height: dim.Height + 2*cfg.MarginPt,
		}
		imp.ScaleAbs = true
		imp.Scale = 1.0
		if err := api.ImportImagesFile([]string{img}, part, imp, Config()); err != nil {
			return fmt.Errorf("图片转 PDF 失败 (%s): %w", filepath.Base(img), err)
		}
		parts = append(parts, part)
	}
	if len(parts) == 1 {
		return copyPDF(parts[0], outPath)
	}
	if err := MergeFiles(parts, outPath); err != nil {
		return fmt.Errorf("合并图片页失败: %w", err)
	}
	return nil
}

func buildImportConfig(cfg ImageImportConfig, _ string) (*pdfcpu.Import, error) {
	imp := pdfcpu.DefaultImportConfig()
	imp.InpUnit = types.POINTS
	imp.Pos = types.Center

	pageSize := string(cfg.PageSize)
	if pageSize == "" || pageSize == PageSizeAuto.String() {
		pageSize = PageSizeA4.String() // 兜底：Auto 不应再走到这里
	}
	imp.PageSize = pageSize
	imp.UserDim = true
	imp.PageDim = types.PaperSize[pageSize]
	if cfg.Landscape {
		imp.PageDim = &types.Dim{Width: imp.PageDim.Height, Height: imp.PageDim.Width}
	}
	if cfg.MarginPt > 0 {
		pw, ph := imp.PageDim.Width, imp.PageDim.Height
		imp.Scale = min((pw-2*cfg.MarginPt)/pw, (ph-2*cfg.MarginPt)/ph)
	} else {
		imp.Scale = 1.0
	}
	return imp, nil
}

func (s PageSizeMode) String() string { return string(s) }

// imageFileDim 读取图片像素尺寸。
func imageFileDim(path string) (types.Dim, error) {
	f, err := os.Open(path)
	if err != nil {
		return types.Dim{}, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return types.Dim{}, err
	}
	return types.Dim{Width: float64(cfg.Width), Height: float64(cfg.Height)}, nil
}

// ImageToSinglePagePDF 将单张图片转为一页 PDF，页面尺寸取 pageW×pageH（pt），
// 图片按等比缩放完整放入页面内（不裁剪）。用于向文档插入图片页时继承目标页尺寸。
func ImageToSinglePagePDF(imgPath string, pageW, pageH float64, outPath string) error {
	if pageW <= 0 || pageH <= 0 {
		return fmt.Errorf("页面尺寸无效")
	}
	if _, err := os.Stat(imgPath); err != nil {
		return fmt.Errorf("图片不存在: %s", filepath.Base(imgPath))
	}
	pixDim, err := imageFileDim(imgPath)
	if err != nil {
		return fmt.Errorf("读取图片尺寸失败: %w", err)
	}
	pixW, pixH := pixDim.Width, pixDim.Height
	if pixW <= 0 || pixH <= 0 {
		return fmt.Errorf("图片尺寸无效")
	}
	// 图片按 96dpi 视觉习惯换算为 pt，等比缩放至恰好放入页面
	iwPt := float64(pixW) * 72.0 / 96.0
	ihPt := float64(pixH) * 72.0 / 96.0
	scale := math.Min(pageW/iwPt, pageH/ihPt)
	if scale > 1 {
		scale = 1
	}

	imp := pdfcpu.DefaultImportConfig()
	imp.InpUnit = types.POINTS
	imp.Pos = types.Center
	imp.UserDim = true
	imp.PageDim = &types.Dim{Width: pageW, Height: pageH}
	imp.Scale = scale
	imp.ScaleAbs = false

	if err := api.ImportImagesFile([]string{imgPath}, outPath, imp, Config()); err != nil {
		return fmt.Errorf("图片转单页 PDF 失败: %w", err)
	}
	return nil
}
