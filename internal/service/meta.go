package service

import (
	"path/filepath"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"pdfstudio/internal/engine"
)

// MetaService 元数据：页标签、附件会话。
type MetaService struct {
	bus   *EventBus
	store *DocStore
}

func NewMetaService(bus *EventBus, store *DocStore) *MetaService {
	return &MetaService{bus: bus, store: store}
}

// AttachmentInfo 附件信息。
type AttachmentInfo struct {
	FileName    string     `json:"fileName"`
	Description string     `json:"description,omitempty"`
	ModTime     *time.Time `json:"modTime,omitempty"`
}

// ListAttachments 列出附件。
func (s *MetaService) ListAttachments(path string) ([]AttachmentInfo, error) {
	if err := checkPDFFile(path); err != nil {
		return nil, err
	}
	aa, err := engine.ListAttachments(path)
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentInfo, 0, len(aa))
	for _, a := range aa {
		out = append(out, AttachmentInfo{FileName: a.FileName, Description: a.Desc, ModTime: a.ModTime})
	}
	return out, nil
}

// RemoveAttachments 移除附件并写入 outPath；files 为空表示全部移除。
// PageLabel 页标签区间（与 PDF number tree 对应）。
type PageLabel struct {
	StartPage  int    `json:"startPage"`        // 区间起始页（0-based）
	Prefix     string `json:"prefix,omitempty"` // 前缀文本
	Style      string `json:"style,omitempty"`  // "" | D | R | r | A | a
	StartValue int    `json:"startValue"`       // 该区间起始编号，默认 1
}

// ReadPageLabels 读取页标签。
func (s *MetaService) ReadPageLabels(path string) ([]PageLabel, error) {
	if err := checkPDFFile(path); err != nil {
		return nil, err
	}
	spec, err := engine.ReadPageLabels(path)
	if err != nil {
		return nil, err
	}
	if spec == nil || len(spec.Ranges) == 0 {
		return nil, nil
	}
	out := make([]PageLabel, 0, len(spec.Ranges))
	for _, r := range spec.Ranges {
		out = append(out, PageLabel{
			StartPage:  r.StartPage,
			Prefix:     r.Prefix,
			Style:      string(r.Style),
			StartValue: r.StartValue,
		})
	}
	return out, nil
}

// writeLabelsFile 把页标签区间写入 outPath（0-based StartPage；供合并管线复用）。
func writeLabelsFile(inPath, outPath string, labels []PageLabel) error {
	if err := checkPDFFile(inPath); err != nil {
		return err
	}
	if filepath.Clean(inPath) == filepath.Clean(outPath) {
		return NewErr("OVERWRITE_FORBIDDEN", "为防止数据损坏，输出文件不能与原文件相同")
	}
	if len(labels) == 0 {
		return NewErr("INVALID_PARAM", "页标签区间不能为空")
	}
	spec := &engine.PageLabelSpec{}
	for _, l := range labels {
		style := engine.LabelStyle(l.Style)
		if !style.Valid() {
			return NewErr("INVALID_PARAM", "页标签样式无效：应为 D/R/r/A/a 或留空")
		}
		spec.Ranges = append(spec.Ranges, engine.PageLabelRange{
			StartPage:  l.StartPage,
			Prefix:     l.Prefix,
			Style:      style,
			StartValue: l.StartValue,
		})
	}
	return engine.WritePageLabels(inPath, outPath, spec)
}

// RemovePageLabels 移除页标签。
var _ = model.Attachment{} // 保持 model 引用（附件类型转换在 ListAttachments 内完成）
