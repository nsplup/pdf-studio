package service

import (
	"sync"
	"time"
)

// AppService 应用生命周期相关：窗口显示控制。

type AppService struct {
	mu           sync.Mutex
	shown        bool
	showFunc     func()
	dirty        bool
	quitFunc     func()
	closeReqFunc func() // 通知前端弹出关闭确认
}

func (s *AppService) SetCloseRequester(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeReqFunc = f
}

// RequestClose 供窗口钩子与 ShouldQuit 调用：通知前端弹出关闭确认。
func (s *AppService) RequestClose() {
	s.mu.Lock()
	f := s.closeReqFunc
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

func (s *AppService) SetUnsavedChanges(v bool) {
	s.mu.Lock()
	s.dirty = v
	s.mu.Unlock()
}

func (s *AppService) HasUnsavedChanges() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirty
}

func (s *AppService) SetQuitFunc(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quitFunc = f
}

// ForceQuit 由前端确认后调用，绕过 dirty 检查直接退出。
func (s *AppService) ForceQuit() {
	s.mu.Lock()
	s.dirty = false
	f := s.quitFunc
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

func NewAppService(show func()) *AppService {
	return &AppService{showFunc: show}
}

// SetShowFunc 后置注入窗口显示函数（窗口在 application.New 之后才能创建）。
func (s *AppService) SetShowFunc(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.showFunc = f
}

// Ready 由前端在挂载完成、首帧渲染后调用，此时才显示窗口，避免启动白屏。
// 重复调用与多次调用均安全。
func (s *AppService) Ready() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown {
		return
	}
	s.shown = true
	if s.showFunc != nil {
		s.showFunc()
	}
}

// Watchdog 兜底：前端异常（如脚本错误导致 Ready 未被调用）时，
// 超时后强制显示窗口，避免用户面对不可见的应用。
func (s *AppService) Watchdog(d time.Duration) {
	go func() {
		time.Sleep(d)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.shown && s.showFunc != nil {
			s.shown = true
			s.showFunc()
		}
	}()
}
