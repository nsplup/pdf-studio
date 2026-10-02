// PDF Studio 桌面应用入口：Wails v3 + pdfcpu + Svelte。
//
// 架构主线：
//   - 服务层（internal/service）是唯一后端暴露面，通过 Wails 绑定给前端调用
//   - 引擎层（internal/engine）集中所有 pdfcpu/go-fitz 调用
//   - 前端（frontend/）只做交互与展示，不解析 PDF
//   - 长任务通过事件总线推送进度（task:update）
package main

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"pdfstudio/internal/server"
	"pdfstudio/internal/service"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 统一日志
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 基础设施
	ws, err := service.NewWorkspace()
	if err != nil {
		logger.Error("初始化工作区失败", "err", err)
		os.Exit(1)
	}
	defer ws.Cleanup()

	bus := service.NewEventBus()
	store := service.NewDocStore(ws)

	// AppService：前端启动就绪后显示窗口（消除启动白屏）
	// 窗口在 application.New 之后才能创建，故 showFunc 采用后置注入
	appService := service.NewAppService(nil)

	// 服务层：唯一暴露面
	documentService := service.NewDocumentService(bus, store)
	mergeService := service.NewMergeService(bus)
	imageService := service.NewImageService(bus, ws)
	outlineService := service.NewOutlineService(bus)
	metaService := service.NewMetaService(bus, store)

	app := application.New(application.Options{
		Name:        "PDF Studio",
		Description: "本地离线 PDF 编辑器",
		Services: []application.Service{
			application.NewService(appService),
			application.NewService(documentService),
			application.NewService(mergeService),
			application.NewService(imageService),
			application.NewService(outlineService),
			application.NewService(metaService),
		},
		Assets: application.AssetOptions{
			// 核心：将 embed 的 frontend/dist 挂入资产服务器，
			// 否则 WebView 加载 "/" 时报 Missing index.html file
			Handler: application.AssetFileServerFS(assets),
			// /thumbs/<docID>/pN.png 由本地临时目录提供（缩略图缓存）
			Middleware: server.AssetsMiddleware(ws.Root(), func(dir, name string) error {
				if service.IsResourceID(dir) {
					var w int
					if _, err := fmt.Sscanf(name, "%d.png", &w); err != nil {
						return err
					}
					return documentService.HealResource(dir, w)
				}
				var pg int
				if _, err := fmt.Sscanf(name, "p%d.png", &pg); err != nil {
					return err
				}
				return documentService.EnsurePageThumb(dir, pg)
			}),
		},
		ShouldQuit: func() bool {
			if !appService.HasUnsavedChanges() {
				return true
			}
			// 异步通知前端；ShouldQuit 本身立即返回 false 阻止退出
			go appService.RequestClose()
			return false
		},
		Logger:   logger,
		LogLevel: slog.LevelInfo,
	})

	// 事件总线接线：服务层进度 -> Wails 事件
	bus.SetEmitter(func(name string, data any) {
		app.Event.Emit(name, data)
	})

	// 退出时清理临时工作区
	app.OnShutdown(func() {
		ws.Cleanup()
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "PDF Studio",
		Width:     1360,
		Height:    880,
		MinWidth:  960,
		MinHeight: 640,
		Hidden:    true, // 白屏优化：前端完全就绪后由 AppService.Ready 显示窗口
		URL:       "/",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarDefault,
		},
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: true,
		},
	})

	// 注入窗口显示函数并启动看门狗（前端异常未调用 Ready 时 8 秒强制显示）
	appService.SetShowFunc(func() { win.Show() })
	appService.Watchdog(8 * time.Second)
	// 注入退出函数
	appService.SetQuitFunc(func() { app.Quit() })
	appService.SetCloseRequester(func() {
		app.Event.Emit("app:close-requested")
	})

	// 窗口关闭钩子：有未保存改动时取消关闭，通知前端弹确认
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !appService.HasUnsavedChanges() {
			return
		}
		e.Cancel()
		appService.RequestClose()
	})
	if err := app.Run(); err != nil {
		logger.Error("应用运行异常", "err", err)
		os.Exit(1)
	}
}
