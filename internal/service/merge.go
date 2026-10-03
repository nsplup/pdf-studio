package service

import (
	"fmt"
	"os"
	"path/filepath"

	"pdfstudio/internal/engine"
)

// MergeService 多 PDF 合并。
type MergeService struct {
	bus *EventBus
}

func NewMergeService(bus *EventBus) *MergeService {
	return &MergeService{bus: bus}
}

// MergeRequest 合并请求。
type MergeRequest struct {
	InputPaths []string `json:"inputPaths"` // 已排序的输入文件
	OutputPath string   `json:"outputPath"`
	SortMode   string   `json:"sortMode"` // "given" 按列表顺序 | "natural" 按文件名自然序重排
}

// Start 异步执行合并；完成事件 result 为输出文件路径。
func (s *MergeService) Start(req MergeRequest) (string, error) {
	if len(req.InputPaths) < 2 {
		return "", NewErr("INVALID_PARAM", "至少选择两个 PDF 文件")
	}
	if req.OutputPath == "" {
		return "", NewErr("INVALID_PARAM", "未指定输出文件")
	}

	inputs := append([]string(nil), req.InputPaths...)
	for _, p := range inputs {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return "", NewErr("FILE_NOT_FOUND", fmt.Sprintf("输入文件不存在: %s", filepath.Base(p)))
		}
	}
	if req.SortMode == "natural" {
		NaturalSortStrings(inputs)
	}

	taskID := "merge-" + randomToken()
	s.bus.Task(taskID, "合并 PDF", func(report func(Progress)) (any, error) {
		total := len(inputs)
		for i, p := range inputs {
			report(Progress{
				Current: i, Total: total,
				Phase: "校验", Detail: filepath.Base(p),
			})
			if _, err := engine.PageCount(p); err != nil {
				return nil, WrapErr("PDF_OPEN_FAILED",
					fmt.Sprintf("文件无法解析: %s", filepath.Base(p)), err)
			}
		}
		report(Progress{Current: total, Total: total, Phase: "写入输出文件"})
		if err := engine.MergeFiles(inputs, req.OutputPath); err != nil {
			return nil, err
		}
		return req.OutputPath, nil
	})
	return taskID, nil
}
