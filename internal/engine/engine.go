// Package engine 是 pdfcpu 与 go-fitz 的适配层。
// 只做原子 PDF 操作，不感知 UI 与业务流程；所有第三方 PDF 调用集中在此，便于测试与替换。
package engine

import (
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func init() {
	// 桌面应用不依赖用户配置目录，避免首次启动写配置文件
	api.DisableConfigDir()
}

// Config 返回统一解析配置（默认已启用 ValidationRelaxed，容忍常见不合规 PDF）。
func Config() *model.Configuration {
	return model.NewDefaultConfiguration()
}
