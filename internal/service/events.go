// Package service 是唯一后端暴露面：参数校验、流程编排、进度发射与错误翻译。
package service

import (
	"fmt"
	"sync"
)

// Progress 任务进度快照。
type Progress struct {
	TaskID  string  `json:"taskId"`
	Current int     `json:"current"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

// TaskUpdate 推送给前端的任务状态。
type TaskUpdate struct {
	TaskID  string  `json:"taskId"`
	Kind    string  `json:"kind"` // progress | done | error
	Label   string  `json:"label,omitempty"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
	Error   string  `json:"error,omitempty"`
	Result  any     `json:"result,omitempty"`
}

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

// Task 异步执行长任务，自动推送 progress/done/error 事件。
// 内部兜底 recover：任何 panic 都会转为 error 事件，保证前端进度浮层不会永久卡住。
func (b *EventBus) Task(taskID, label string, work func(report func(current, total int, msg string)) (any, error)) {
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
		report := func(current, total int, msg string) {
			pct := 0.0
			if total > 0 {
				pct = float64(current) / float64(total) * 100
			}
			b.Emit("task:update", TaskUpdate{
				TaskID: taskID, Kind: "progress",
				Label: label, Percent: pct, Message: msg,
			})
		}
		report(0, 1, label)
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
