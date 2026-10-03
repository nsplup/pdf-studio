// Package service 是唯一后端暴露面：参数校验、流程编排、进度发射与错误翻译。
package service

import (
	"fmt"
	"sync"
)

// EventBus 把服务层事件桥接到 Wails 事件系统；服务本身不依赖 Wails。
type EventBus struct {
	mu   sync.RWMutex
	emit func(name string, data any)
}

func NewEventBus() *EventBus { return &EventBus{} }

// SetEmitter 在 Wails 应用创建后注入真正的发射函数。
func (b *EventBus) SetEmitter(f func(name string, data any)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.emit = f
}

// Emit 发射事件；未接线时静默丢弃（纯后端测试模式）。
func (b *EventBus) Emit(name string, data any) {
	b.mu.RLock()
	f := b.emit
	b.mu.RUnlock()
	if f != nil {
		f(name, data)
	}
}

// Progress 一次进度上报的结构化载荷。
// Phase 用于前端分组与阶段切换检测；Detail 承载易变细节（文件名等）；
// message 不再由后端拼装，交给前端按 phase + detail 渲染。
type Progress struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Phase   string `json:"phase"`
	Detail  string `json:"detail,omitempty"`
}

type TaskUpdate struct {
	TaskID  string  `json:"taskId"`
	Kind    string  `json:"kind"`
	Label   string  `json:"label,omitempty"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
	Phase   string  `json:"phase,omitempty"`
	Detail  string  `json:"detail,omitempty"`
	Error   string  `json:"error,omitempty"`
	Result  any     `json:"result,omitempty"`
}

func (b *EventBus) Task(taskID, label string, work func(report func(Progress)) (any, error)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.Emit("task:update", TaskUpdate{
					TaskID: taskID, Kind: "error",
					Label: label, Percent: 100, Message: label,
					Error: fmt.Sprintf("内部错误：%v", r),
				})
			}
		}()
		report := func(p Progress) {
			pct := 0.0
			if p.Total > 0 {
				pct = float64(p.Current) / float64(p.Total) * 100
			}
			b.Emit("task:update", TaskUpdate{
				TaskID: taskID, Kind: "progress",
				Label: label, Percent: pct,
				Phase: p.Phase, Detail: p.Detail,
				Message: p.Phase, // 兜底：前端未识别 phase 时仍能显示
			})
		}
		report(Progress{Phase: label})
		result, err := work(report)
		if err != nil {
			b.Emit("task:update", TaskUpdate{
				TaskID: taskID, Kind: "error",
				Label: label, Percent: 100, Message: label,
				Error: UserMessage(err),
			})
			return
		}
		b.Emit("task:update", TaskUpdate{
			TaskID: taskID, Kind: "done",
			Label: label, Percent: 100, Message: label + " 完成",
			Result: result,
		})
	}()
}

// UserMessage 统一错误翻译：底层错误转用户可读文案，不暴露原始堆栈。
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := err.(*AppError); ok {
		if ae.Detail != "" {
			return fmt.Sprintf("%s（%s）", ae.Message, ae.Detail)
		}
		return ae.Message
	}
	return err.Error()
}

// AppError 统一错误模型。
type AppError struct {
	Code    string // 稳定错误码，供前端分支处理
	Message string // 用户可读信息
	Detail  string // 可选补充信息
}

func (e *AppError) Error() string { return e.Message }

func NewErr(code, msg string) *AppError { return &AppError{Code: code, Message: msg} }

func WrapErr(code, msg string, err error) *AppError {
	return &AppError{Code: code, Message: msg, Detail: err.Error()}
}

var (
	ErrDocNotFound  = func(id string) error { return NewErr("DOC_NOT_FOUND", "文档不存在或已关闭") }
	ErrInvalidParam = NewErr("INVALID_PARAM", "参数无效")
)
