package service

import (
	"sync"
	"time"
)

// AppService 应用生命周期相关：窗口显示控制。
type AppService struct {
	mu       sync.Mutex
	shown    bool
	showFunc func()
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
