package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ---------- 附件 ----------

// ListAttachments 列出内嵌附件。
func ListAttachments(path string) ([]model.Attachment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()
	aa, err := api.Attachments(f, Config())
	if err != nil {
		return nil, fmt.Errorf("读取附件列表失败: %w", err)
	}
	return aa, nil
}

// RemoveAttachments 移除指定名称附件；files 为空切片时移除全部。
func RemoveAttachments(inPath, outPath string, files []string) error {
	if len(files) == 0 {
		aa, err := ListAttachments(inPath)
		if err != nil {
			return err
		}
		if len(aa) == 0 {
			return fmt.Errorf("文档没有附件")
		}
		for _, a := range aa {
			files = append(files, a.FileName)
		}
	}
	if err := api.RemoveAttachmentsFile(inPath, outPath, files, Config()); err != nil {
		return fmt.Errorf("移除附件失败: %w", err)
	}
	return nil
}

// ---------- 页标签（Page Labels） ----------

// LabelStyle 页标签编号风格。
type LabelStyle string

const (
	StyleNone   LabelStyle = ""  // 无编号，仅前缀
	StyleDigit  LabelStyle = "D" // 1,2,3
	StyleUpper  LabelStyle = "R" // I,II,III
	StyleLower  LabelStyle = "r" // i,ii,iii
	StyleAlphaU LabelStyle = "A" // A,B,C
	StyleAlphaL LabelStyle = "a" // a,b,c
)

// Valid 校验样式取值。
func (s LabelStyle) Valid() bool {
	switch s {
	case StyleNone, StyleDigit, StyleUpper, StyleLower, StyleAlphaU, StyleAlphaL:
		return true
	}
	return false
}

// PageLabelRange 描述从 StartPage（0-based）开始的一组页标签规则。
type PageLabelRange struct {
	StartPage  int        `json:"startPage"`        // 0-based
	Prefix     string     `json:"prefix,omitempty"` // 静态前缀
	Style      LabelStyle `json:"style,omitempty"`  // 编号风格
	StartValue int        `json:"startValue"`       // 起始编号，默认 1
}

// PageLabelSpec 页标签整体规格。
type PageLabelSpec struct {
	Ranges []PageLabelRange `json:"ranges"`
}

// ReadPageLabels 读取文档页标签 number tree，无页标签时返回 nil spec。
func ReadPageLabels(path string) (*PageLabelSpec, error) {
	ctx, err := api.ReadContextFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文档失败: %w", err)
	}
	obj, found := ctx.RootDict.Find("PageLabels")
	if !found {
		return nil, nil
	}
	resolved, err := ctx.Dereference(obj)
	if err != nil {
		return nil, fmt.Errorf("解析页标签失败: %w", err)
	}
	dict, ok := resolved.(types.Dict)
	if !ok {
		return nil, fmt.Errorf("页标签结构异常")
	}
	ranges := []PageLabelRange{}
	collectLabelRanges(ctx, dict, &ranges)
	if len(ranges) == 0 {
		return nil, nil
	}
	return &PageLabelSpec{Ranges: ranges}, nil
}

// collectLabelRanges 递归解析 number tree 节点。
func collectLabelRanges(ctx *model.Context, dict types.Dict, out *[]PageLabelRange) {
	if kidsObj, found := dict.Find("Kids"); found {
		if arr, err := ctx.DereferenceArray(kidsObj); err == nil {
			for _, k := range arr {
				kid, err := ctx.DereferenceDict(k)
				if err != nil {
					continue
				}
				collectLabelRanges(ctx, kid, out)
			}
			return
		}
	}
	numsObj, found := dict.Find("Nums")
	if !found {
		return
	}
	arr, err := ctx.DereferenceArray(numsObj)
	if err != nil {
		return
	}
	for i := 0; i+1 < len(arr); i += 2 {
		startPage, err := ctx.DereferenceInteger(arr[i])
		if err != nil {
			continue
		}
		labDict, err := ctx.DereferenceDict(arr[i+1])
		if err != nil {
			continue
		}
		r := PageLabelRange{StartPage: startPage.Value(), StartValue: 1}
		if p, found := labDict.Find("P"); found {
			if s, err := ctx.DereferenceStringOrHexLiteral(p, model.V13, nil); err == nil {
				r.Prefix = s
			}
		}
		if s, found := labDict.Find("S"); found {
			if n, err := ctx.DereferenceName(s, model.V13, nil); err == nil {
				r.Style = LabelStyle(n.Value())
			}
		}
		if st, found := labDict.Find("St"); found {
			if iv, err := ctx.DereferenceInteger(st); err == nil {
				r.StartValue = iv.Value()
			}
		}
		*out = append(*out, r)
	}
}

// WritePageLabels 以扁平 Nums 树写入页标签，写入 outPath 新文件。
func WritePageLabels(inPath, outPath string, spec *PageLabelSpec) error {
	if spec == nil || len(spec.Ranges) == 0 {
		return fmt.Errorf("页标签规格为空")
	}
	ctx, err := api.ReadContextFile(inPath)
	if err != nil {
		return fmt.Errorf("读取文档失败: %w", err)
	}
	// PDF 规范要求 Nums 键严格升序，此处按起始页稳定排序，乱序输入也可生成合法树。
	ranges := append([]PageLabelRange(nil), spec.Ranges...)
	sort.SliceStable(ranges, func(i, j int) bool { return ranges[i].StartPage < ranges[j].StartPage })
	nums := types.Array{}
	for _, r := range ranges {
		lab := types.NewDict()
		if r.Prefix != "" {
			lab.Insert("P", types.StringLiteral(escapeString(r.Prefix)))
		}
		if r.Style != "" {
			lab.Insert("S", types.Name(r.Style))
		}
		if r.StartValue > 1 {
			lab.Insert("St", types.Integer(r.StartValue))
		}
		nums = append(nums, types.Integer(r.StartPage), lab)
	}
	pls := types.NewDict()
	pls.Insert("Nums", nums)
	// 注意：pdfcpu 的 Dict.Insert 是「仅当键不存在时才写入」（insert-if-absent），
	// 对已有页标签的文档使用 Insert 会静默跳过、保留旧标签且不报错，
	// 导致「保存成功但页标签不生效」。这里必须用 Update 无条件覆盖。
	ctx.RootDict.Update("PageLabels", pls)

	if err := api.WriteContextFile(ctx, outPath); err != nil {
		return fmt.Errorf("写入文档失败: %w", err)
	}
	return nil
}

// RemovePageLabels 移除页标签定义。
func RemovePageLabels(inPath, outPath string) error {
	ctx, err := api.ReadContextFile(inPath)
	if err != nil {
		return fmt.Errorf("读取文档失败: %w", err)
	}
	if _, found := ctx.RootDict.Find("PageLabels"); !found {
		return fmt.Errorf("文档没有页标签")
	}
	ctx.RootDict.Delete("PageLabels")
	if err := api.WriteContextFile(ctx, outPath); err != nil {
		return fmt.Errorf("写入文档失败: %w", err)
	}
	return nil
}

// escapeString 转义 PDF 字面字符串中的括号与反斜杠。
func escapeString(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' || c == ')' || c == '\\' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return string(out)
}

// AddAttachments 将本地文件作为附件写入新文件（同名覆盖更新）。
func AddAttachments(inPath, outPath string, files []string) error {
	if err := api.AddAttachmentsFile(inPath, outPath, files, false, Config()); err != nil {
		return fmt.Errorf("添加附件失败: %w", err)
	}
	return nil
}

// ExtractAttachment 提取单个附件文件到 outDir，返回落盘路径。
func ExtractAttachment(inPath, fileName, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建附件目录失败: %w", err)
	}
	if err := api.ExtractAttachmentsFile(inPath, outDir, []string{fileName}, Config()); err != nil {
		return "", fmt.Errorf("提取附件失败: %w", err)
	}
	out := filepath.Join(outDir, fileName)
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("附件未落盘: %s", fileName)
	}
	return out, nil
}
