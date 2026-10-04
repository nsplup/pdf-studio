package engine

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// PageCount 返回 PDF 总页数。
func PageCount(path string) (int, error) {
	n, err := api.PageCountFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取页数失败: %w", err)
	}
	return n, nil
}

// PageDims 返回各页面尺寸（单位 pt），用于缩略图长宽比与空白页默认尺寸。
func PageDims(path string) ([]types.Dim, error) {
	dims, err := api.PageDimsFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取页面尺寸失败: %w", err)
	}
	return dims, nil
}

// RemovePages 删除 selectedPages 指定页（如 "1-3,5"），写入 outPath。
// removePagesWithFallback 删除页面：优先直删；因命名目标迁移缺陷失败时，
// 剥离命名目标树后重试（副作用：GoTo 链接退化为直接页引用或失效）。
func removePagesWithFallback(inPath, outPath, selectedPages string, sel []string) error {
	err := api.RemovePagesFile(inPath, outPath, sel, Config())
	if err == nil {
		return nil
	}
	dir := filepath.Dir(outPath)
	clean := filepath.Join(dir, fmt.Sprintf(".rm-clean-%d.pdf", time.Now().UnixNano()))
	cleanupFile(clean)
	if serr := stripNamedDests(inPath, clean); serr != nil {
		return fmt.Errorf("删除页面失败: %w", err)
	}
	defer cleanupFile(clean)
	if err2 := api.RemovePagesFile(clean, outPath, sel, Config()); err2 != nil {
		return fmt.Errorf("删除页面失败: %w", err)
	}
	return nil
}

func RemovePages(inPath, outPath, selectedPages string) error {
	if err := removePagesWithFallback(inPath, outPath, selectedPages, splitSelection(selectedPages)); err != nil {
		return err
	}
	return nil
}

// TrimPages 仅保留 selectedPages 指定页，写入 outPath。
// 命名目标迁移失败时剥离命名目标树后重试
// （副作用：GoTo 链接退化为直接页引用或失效）。
func TrimPages(inPath, outPath, selectedPages string) error {
	sel := splitSelection(selectedPages)
	err := api.TrimFile(inPath, outPath, sel, Config())
	if err == nil {
		return nil
	}

	dir := filepath.Dir(outPath)
	clean := filepath.Join(dir, fmt.Sprintf(".trim-clean-%d.pdf", time.Now().UnixNano()))
	cleanupFile(clean)
	defer cleanupFile(clean)

	if serr := stripNamedDests(inPath, clean); serr != nil {
		// 剥离也失败：把原始错误抛给上层，便于定位
		return fmt.Errorf("裁剪页面失败: %w", err)
	}
	if err2 := api.TrimFile(clean, outPath, sel, Config()); err2 != nil {
		return fmt.Errorf("裁剪页面失败: %w", err2)
	}
	return nil
}

// ValidatePageSelection 预校验页选择表达式在 1..pageCount 范围内。
// 支持 "3"、"1-5"、"1-5,8,10-"（10- 表示到末尾）等，pdfcpu 侧仍会做完整校验。
func ValidatePageSelection(pageCount int, selection string) error {
	if selection == "" || selection == "*" {
		return nil
	}
	for _, part := range splitComma(selection) {
		if part == "" {
			continue
		}
		if i := indexByte(part, '-'); i >= 0 {
			lo := part[:i]
			hi := part[i+1:]
			if lo != "" {
				if n, ok := atoi(lo); !ok || n < 1 || n > pageCount {
					return fmt.Errorf("页码 %s 超出范围 (1-%d)", lo, pageCount)
				}
			}
			if hi != "" {
				if n, ok := atoi(hi); !ok || n < 1 || n > pageCount {
					return fmt.Errorf("页码 %s 超出范围 (1-%d)", hi, pageCount)
				}
			}
		} else {
			if n, ok := atoi(part); !ok || n < 1 || n > pageCount {
				return fmt.Errorf("页码 %s 超出范围 (1-%d)", part, pageCount)
			}
		}
	}
	return nil
}

// InsertBlankPages 在第 pageIndex 页（1-based）之前/之后插入 count 张空白页。
// dim 为 nil 时使用文档相邻页尺寸（由服务层决定），否则使用指定尺寸。
func InsertBlankPages(inPath, outPath string, pageIndex, count int, before bool, dim *types.Dim) error {
	if count < 1 {
		return fmt.Errorf("插入数量至少为 1")
	}
	pageConf := &pdfcpu.PageConfiguration{InpUnit: types.POINTS}
	if dim != nil {
		pageConf.UserDim = true
		pageConf.PageDim = dim
	}
	sel := fmt.Sprintf("%d", pageIndex)
	// pdfcpu 每次 插入一张；重复执行 count 次即可在锚点连续插入
	current := inPath
	var iterErr error
	for i := 0; i < count; i++ {
		out := outPath
		if i < count-1 {
			out = TempPath(fmt.Sprintf("blank-%d.pdf", i))
		}
		if err := api.InsertPagesFile(current, out, []string{sel}, before, pageConf, Config()); err != nil {
			iterErr = fmt.Errorf("插入空白页失败: %w", err)
			break
		}
		if current != inPath {
			removeFile(current)
		}
		current = out
	}
	return iterErr
}

// InsertFromPDF 将 srcPath 中 srcPages 指定的页插入到 inPath 的 atIndex 位置（1-based， atIndex=1 表示最前）。
// 实现方式：前后分段裁剪 + 三段合并，书签由 pdfcpu 合并时自动保留。
//
// 命名目标兜底：图片导入路径生成的孤儿命名目标（xref 不完整）会让 pdfcpu
// 的命名目标迁移报 "no xref entry found"。此处对分段裁剪与最终合并两步都
// 做兜底——失败时剥离相关文件的命名目标树后重试，使插入操作能完成。
// 副作用：源文件的命名目标丢失，直接页引用不受影响。
func InsertFromPDF(inPath string, atIndex int, srcPath, srcPages string, outPath string) error {
	conf := Config()
	total, err := PageCount(inPath)
	if err != nil {
		return err
	}
	if atIndex < 1 || atIndex > total+1 {
		return fmt.Errorf("插入位置 %d 超出范围 (1-%d)", atIndex, total+1)
	}

	tmp := &TempNames{}
	defer tmp.Cleanup()

	// stripped 缓存：同一源文件只需剥离一次命名目标树。
	stripped := map[string]string{}
	cleanOf := func(src string) (string, error) {
		if p, ok := stripped[src]; ok {
			return p, nil
		}
		p := tmp.New(fmt.Sprintf("clean-%d.pdf", len(stripped)))
		if err := stripNamedDests(src, p); err != nil {
			return "", err
		}
		stripped[src] = p
		return p, nil
	}
	// trim 封装 api.TrimFile；命名目标迁移失败时，改用剥离后的源文件重试。
	trim := func(src, out string, pages []string, what string) error {
		trimErr := api.TrimFile(src, out, pages, conf)
		if trimErr == nil {
			return nil
		}
		clean, cerr := cleanOf(src)
		if cerr != nil {
			// 剥离也失败：保留 pdfcpu 原始诊断信息，便于定位根因。
			return fmt.Errorf("%s失败: %w", what, trimErr)
		}
		if err := api.TrimFile(clean, out, pages, conf); err != nil {
			return fmt.Errorf("%s失败: %w", what, err)
		}
		return nil
	}

	var headPath, tailPath, srcPath2 string
	var parts []string

	if atIndex > 1 {
		headPath = tmp.New("head.pdf")
		if err := trim(inPath, headPath, []string{fmt.Sprintf("1-%d", atIndex-1)}, "分段"); err != nil {
			return err
		}
		parts = append(parts, headPath)
	}

	srcPath2 = srcPath
	if srcPages != "" && srcPages != "*" && srcPages != "1-*" {
		srcPath2 = tmp.New("src.pdf")
		if err := trim(srcPath, srcPath2, splitSelection(srcPages), "提取来源页"); err != nil {
			return err
		}
	}
	parts = append(parts, srcPath2)

	if atIndex <= total {
		tailPath = tmp.New("tail.pdf")
		if err := trim(inPath, tailPath, []string{fmt.Sprintf("%d-%d", atIndex, total)}, "分段"); err != nil {
			return err
		}
		parts = append(parts, tailPath)
	}

	// 合并：srcPages=="*" 时 srcPath2 就是原始 srcPath，未经 trim，
	// 其中的孤儿命名目标会在 MergeCreateFile 里再次触发迁移失败。
	// 失败时对每个片段剥离命名目标树后重试。parts 均为临时文件，
	// 剥离不影响用户原始数据。
	mergeErr := api.MergeCreateFile(parts, outPath, false, conf)
	if mergeErr == nil {
		return nil
	}
	cleaned := make([]string, 0, len(parts))
	for i, p := range parts {
		cp := tmp.New(fmt.Sprintf("mclean-%d.pdf", i))
		if err := stripNamedDests(p, cp); err != nil {
			return fmt.Errorf("合并写入失败: %w", mergeErr)
		}
		cleaned = append(cleaned, cp)
	}
	if err := api.MergeCreateFile(cleaned, outPath, false, conf); err != nil {
		return fmt.Errorf("合并写入失败: %w", err)
	}
	return nil
}

// TempNames 管理一组临时文件名，统一清理。
type TempNames struct{ names []string }

func (t *TempNames) New(name string) string {
	p := TempPath(name)
	t.names = append(t.names, p)
	return p
}

func (t *TempNames) Cleanup() {
	for _, p := range t.names {
		removeFile(p)
	}
}

// BlankPageDimFor 返回与第 pageNr 页（1-based，越界取末页）相同尺寸的空白页尺寸。
func BlankPageDimFor(path string, pageNr int) (*types.Dim, error) {
	dims, err := PageDims(path)
	if err != nil || len(dims) == 0 {
		return nil, err
	}
	if pageNr < 1 {
		pageNr = 1
	}
	if pageNr > len(dims) {
		pageNr = len(dims)
	}
	d := dims[pageNr-1]
	return &d, nil
}

var _ = model.VersionStr // 保持 model 引用

// parseSelection 解析页码选择为升序页号列表（支持 "*"、"N"、"lo-hi"、逗号组合）。
// ParseSelection 把页选择表达式解析为去重升序页号列表（1-based），供服务层复用。
func ParseSelection(total int, selection string) ([]int, error) {
	if selection == "" {
		return nil, fmt.Errorf("未指定页码")
	}
	seen := map[int]bool{}
	var out []int
	add := func(n int) {
		if n >= 1 && n <= total && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if selection == "*" {
		for i := 1; i <= total; i++ {
			add(i)
		}
		return out, nil
	}
	for _, part := range splitComma(selection) {
		if part == "" {
			continue
		}
		if i := indexByte(part, '-'); i >= 0 {
			lo, lok := atoi(part[:i])
			hi, hik := atoi(part[i+1:])
			if !lok || !hik || lo < 1 || hi < lo || hi > total {
				return nil, fmt.Errorf("页码范围 %s 无效 (1-%d)", part, total)
			}
			for n := lo; n <= hi; n++ {
				add(n)
			}
		} else {
			n, ok := atoi(part)
			if !ok || n < 1 || n > total {
				return nil, fmt.Errorf("页码 %s 无效 (1-%d)", part, total)
			}
			add(n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("页码选择为空")
	}
	return out, nil
}

// selectionString 把页号列表转为 "2,5-7" 风格的紧凑表达（连续段合并）。
func selectionString(pages []int) string {
	if len(pages) == 0 {
		return ""
	}
	var parts []string
	start, prev := pages[0], pages[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, n := range pages[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()
	return strings.Join(parts, ",")
}

// MovePages 将选中页按原顺序移动到 atIndex 位置（1-based，基于移动前编号；
// before=true 表示插在原第 atIndex 页之前）。输出写入 outPath。
// 实现方式：抽取选中页 → 从原文档移除 → 按修正后的位置回插。
func MovePages(inPath, pages string, atIndex int, before bool, outPath string) error {
	total, err := PageCount(inPath)
	if err != nil {
		return err
	}
	if err := ValidatePageSelection(total, pages); err != nil {
		return err
	}
	sel, err := ParseSelection(total, pages)
	if err != nil {
		return err
	}
	if len(sel) == total {
		return fmt.Errorf("不能移动全部页面")
	}
	if atIndex < 1 || atIndex > total+1 {
		return fmt.Errorf("目标位置 %d 超出范围 (1-%d)", atIndex, total+1)
	}

	// 目标位置修正：移除选中页后，原编号 >= 插入点的页会前移
	shift := 0
	for _, n := range sel {
		if before && n < atIndex {
			shift++
		} else if !before && n <= atIndex {
			shift++
		}
	}
	eff := atIndex - shift
	if eff < 1 {
		eff = 1
	}

	dir := filepath.Dir(outPath)
	tmpSel := filepath.Join(dir, fmt.Sprintf(".mv-sel-%d.pdf", time.Now().UnixNano()))
	tmpRem := filepath.Join(dir, fmt.Sprintf(".mv-rem-%d.pdf", time.Now().UnixNano()))
	defer os.Remove(tmpSel)
	defer os.Remove(tmpRem)

	// 选中页单页档：复制原文档后删除补集
	comp := []int{}
	selSet := map[int]bool{}
	for _, n := range sel {
		selSet[n] = true
	}
	for i := 1; i <= total; i++ {
		if !selSet[i] {
			comp = append(comp, i)
		}
	}
	// 带命名目标迁移缺陷兜底的页抽取
	trim := func(src, out string, pages []int, what string) error {
		// pdfcpu 的页选择每个 token 只能是单个页号或区间（不含逗号），
		// 必须 "1-10,15-319" 拆分为 ["1-10","15-319"]
		tokens := splitSelection(selectionString(pages))
		if err := api.TrimFile(src, out, tokens, Config()); err == nil {
			return nil
		}
		clean := filepath.Join(dir, fmt.Sprintf(".mv-clean-%d.pdf", time.Now().UnixNano()))
		if serr := stripNamedDests(src, clean); serr != nil {
			cleanupFile(clean)
			return fmt.Errorf("%s失败: %w", what, err)
		}
		defer cleanupFile(clean)
		if err := api.TrimFile(clean, out, tokens, Config()); err != nil {
			return fmt.Errorf("%s失败: %w", what, err)
		}
		return nil
	}
	// 选中页单页档：仅保留选中页
	if err := trim(inPath, tmpSel, sel, "抽取选中页"); err != nil {
		return err
	}
	// 剩余页文档：仅保留未选中页
	if err := trim(inPath, tmpRem, comp, "移除选中页"); err != nil {
		return err
	}
	// 回插
	if err := InsertFromPDF(tmpRem, eff, tmpSel, "*", outPath); err != nil {
		return fmt.Errorf("插入选中页失败: %w", err)
	}
	return nil
}

// copyPDF 复制 PDF 文件内容。
func copyPDF(src, dst string) error {
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
	_, err = io.Copy(out, in)
	return err
}

// Piece 保存装配的最小页面单元：普通页来自 Path 的第 Page 页；空白页由 BlankDim 描述。
type Piece struct {
	Path     string
	Page     int
	BlankDim *types.Dim
}

type asmRun struct {
	path  string
	from  int
	to    int
	blank *types.Dim
}

// writeWhitePNG 写一张 4x4 白色 PNG（用于生成空白页）。
func writeWhitePNG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// CreateBlankPagePDF 生成单页空白 PDF（尺寸 pt）。
func CreateBlankPagePDF(dim types.Dim, outPath string) error {
	tmp := TempPath("blank-src.png")
	defer removeFile(tmp)
	if err := writeWhitePNG(tmp); err != nil {
		return fmt.Errorf("生成空白页失败: %w", err)
	}
	if err := ImageToSinglePagePDF(tmp, dim.Width, dim.Height, outPath); err != nil {
		return fmt.Errorf("生成空白页失败: %w", err)
	}
	return nil
}

// AssemblePages 按 pieces 顺序装配新 PDF。
// 相同源文件且页码连续的页面合并为区间处理（裁剪 + 合并），空白页单独生成。
func AssemblePages(pieces []Piece, outPath string) error {
	runs := make([]asmRun, 0, len(pieces))
	for _, p := range pieces {
		if p.BlankDim != nil {
			runs = append(runs, asmRun{blank: p.BlankDim})
			continue
		}
		if n := len(runs); n > 0 && runs[n-1].blank == nil && runs[n-1].path == p.Path && runs[n-1].to+1 == p.Page {
			runs[n-1].to = p.Page
			continue
		}
		runs = append(runs, asmRun{path: p.Path, from: p.Page, to: p.Page})
	}
	parts := make([]string, 0, len(runs))
	defer func() {
		for _, p := range parts {
			removeFile(p)
		}
	}()
	for i, r := range runs {
		tmp := TempPath(fmt.Sprintf("asm-%d.pdf", i))
		if r.blank != nil {
			if err := CreateBlankPagePDF(*r.blank, tmp); err != nil {
				return err
			}
		} else {
			sel := fmt.Sprintf("%d", r.from)
			if r.to > r.from {
				sel = fmt.Sprintf("%d-%d", r.from, r.to)
			}
			if err := TrimPages(r.path, tmp, sel); err != nil {
				return fmt.Errorf("装配页面失败: %w", err)
			}
		}
		parts = append(parts, tmp)
	}
	if err := MergeFiles(parts, outPath); err != nil {
		return fmt.Errorf("装配页面失败: %w", err)
	}
	return nil
}
