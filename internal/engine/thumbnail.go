package engine

import (
        "fmt"
        "os"
        "path/filepath"

        "github.com/gen2brain/go-fitz"
)

// RenderPagePNG 将第 pageNr 页（1-based）渲染为 PNG 字节。
// targetWidth 为期望像素宽度，内部据此换算 DPI。
func RenderPagePNG(pdfPath string, pageNr int, targetWidth int) ([]byte, error) {
        if pageNr < 1 {
                return nil, fmt.Errorf("页码无效: %d", pageNr)
        }
        doc, err := fitz.New(pdfPath)
        if err != nil {
                return nil, fmt.Errorf("打开文档渲染失败: %w", err)
        }
        defer doc.Close()

        n := doc.NumPage()
        if n <= 0 {
                return nil, fmt.Errorf("文档没有可渲染页面")
        }
        if pageNr > n {
                return nil, fmt.Errorf("页码 %d 超出总页数 %d", pageNr, n)
        }

        bound, err := doc.Bound(pageNr - 1)
        if err != nil {
                return nil, fmt.Errorf("读取页面尺寸失败: %w", err)
        }
        // MuPDF 基准 72dpi；bound 单位为 pt
        dpi := 72.0
        if bound.Dx() > 0 {
                dpi = 72.0 * float64(targetWidth) / float64(bound.Dx())
        }
        if dpi > 300 {
                dpi = 300 // 封顶，避免超大页面耗尽内存
        }
        png, err := doc.ImagePNG(pageNr-1, dpi)
        if err != nil {
                return nil, fmt.Errorf("渲染页面失败: %w", err)
        }
        return png, nil
}

// RenderThumbnailsDir 为文档生成缩略图到 outDir，命名 p%d.png。
// fromPage（1-based）之前的页保留已有文件（增量重渲染：页面编辑只影响受影响页），
// 并清理超过总页数的残留文件。onPage 每完成一页回调（done 从 1 开始）；为 nil 时静默。
func RenderThumbnailsDir(pdfPath, outDir string, width, fromPage int, onPage func(done, total int)) (count int, err error) {
        if fromPage < 1 {
                fromPage = 1
        }
        if err = os.MkdirAll(outDir, 0o755); err != nil {
                return 0, fmt.Errorf("创建缩略图目录失败: %w", err)
        }
        doc, err := fitz.New(pdfPath)
        if err != nil {
                return 0, fmt.Errorf("打开文档渲染失败: %w", err)
        }
        defer doc.Close()
        n := doc.NumPage()
        // 清理页数减少后残留的旧文件
        for i := n + 1; i < n+50; i++ {
                p := filepath.Join(outDir, fmt.Sprintf("p%d.png", i))
                if _, serr := os.Stat(p); serr != nil {
                        break
                }
                _ = os.Remove(p)
        }
        for i := fromPage - 1; i < n; i++ {
                bound, berr := doc.Bound(i)
                dpi := 72.0
                if berr == nil && bound.Dx() > 0 {
                        dpi = 72.0 * float64(width) / float64(bound.Dx())
                }
                if dpi > 300 {
                        dpi = 300
                }
                png, rerr := doc.ImagePNG(i, dpi)
                if rerr != nil {
                        return i, fmt.Errorf("渲染第 %d 页失败: %w", i+1, rerr)
                }
                out := filepath.Join(outDir, fmt.Sprintf("p%d.png", i+1))
                if werr := os.WriteFile(out, png, 0o644); werr != nil {
                        return i, fmt.Errorf("写入缩略图失败: %w", werr)
                }
                if onPage != nil {
                        onPage(i+1, n)
                }
        }
        return n, nil
}

// RenderPagePNGToFile 渲染单页缩略图并写入 outPath（同步，用于封面/单页预览）。
func RenderPagePNGToFile(pdfPath string, pageNr, targetWidth int, outPath string) error {
        data, err := RenderPagePNG(pdfPath, pageNr, targetWidth)
        if err != nil {
                return err
        }
        if err := os.WriteFile(outPath, data, 0o644); err != nil {
                return fmt.Errorf("写入缩略图失败: %w", err)
        }
        return nil
}
