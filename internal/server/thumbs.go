// Package server 提供缩略图等本地静态资源的 HTTP 服务，
// 挂载在 Wails asset server 的中间件上：
//   - /thumbs/<docID>/p<N>.png  文档页面缩略图（引擎渲染缓存）
//   - /imgs/<sessionID>/<name>  图片会话资源（图片转 PDF 面板的预览）
package server

import (
	"fmt"
	"net/http"

	"os"
	"path/filepath"
	"pdfstudio/internal/service"
	"strings"
)

var servedExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".tif": true, ".tiff": true, ".bmp": true, ".webp": true,
}

// resolveToken 校验单段路径元素：不含分隔符、非 ".."、非空。
func resolveToken(s, prefix string) (string, bool) {
	if s == "" || !strings.HasPrefix(s, prefix) {
		return "", false
	}
	if strings.ContainsAny(s, "/\\") || s == "." || s == ".." || strings.Contains(s, "\x00") {
		return "", false
	}
	return s, true
}

// handler 直接以文件方式响应，避免 http.FileServer 的路径映射歧义（404 根因）。
type staticFiles struct {
	root        string
	validateDir func(relDir string) bool     // 对第一段目录名的额外校验
	healDir     func(dir, name string) error // 可选：资源缺失自愈
}

func (h staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := filepath.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	rel = strings.TrimPrefix(rel, "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	dir, name := parts[0], parts[1]
	if !h.validateDir(dir) {
		http.NotFound(w, r)
		return
	}
	if filepath.Ext(name) != ".png" && !servedExtensions[filepath.Ext(name)] {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(h.root, dir, name)
	st, err := os.Stat(full)
	if (err != nil || st.IsDir()) && h.healDir != nil {
		// 缺失资源按需渲染自愈：
		//   - <docID>/p<N>.png（遗留文档寻址）
		//   - <uuid>/<W>.png（内容寻址，如预览 900px）
		var pg int
		ok := false
		if _, e1 := fmt.Sscanf(name, "p%d.png", &pg); e1 == nil {
			ok = true
		} else if _, e2 := fmt.Sscanf(name, "%d.png", &pg); e2 == nil && h.validateDir(dir) {
			ok = true
		}
		if ok {
			if herr := h.healDir(dir, name); herr == nil {
				st, err = os.Stat(full)
			}
		}
	}
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, full)
}

// NewThumbsHandler 返回静态页面资源服务：
//   - 内容寻址：<root>/thumbs/<uuid>/<W>.png（uuid = sha1 hex，页面内容资源）；
//   - 兼容遗留：<root>/thumbs/<docID>/p<N>.png（180px 文档缩略图）。
//
// heal 非空时：缺失资源先按需渲染再响应（自愈）。
func NewThumbsHandler(root string, heal func(dir, name string) error) http.Handler {
	return staticFiles{
		root: filepath.Join(root, "thumbs"),
		validateDir: func(d string) bool {
			if _, ok := resolveToken(d, "doc-"); ok {
				return true
			}
			return service.IsResourceID(d)
		},
		healDir: heal,
	}
}

// NewImagesHandler 返回图片会话资源服务：<root>/imgs/<sessionID>/<file>。
func NewImagesHandler(root string) http.Handler {
	return staticFiles{
		root: filepath.Join(root, "imgs"),
		validateDir: func(d string) bool {
			_, ok := resolveToken(d, "img-")
			return ok
		},
	}
}

// AssetsMiddleware 将 /thumbs/ 与 /imgs/ 请求分流到本地静态资源，其余交给默认 asset 链。
func AssetsMiddleware(root string, heal func(dir, name string) error) func(http.Handler) http.Handler {
	thumbs := NewThumbsHandler(root, heal)
	imgs := NewImagesHandler(root)
	strip := func(r *http.Request, prefix string) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, "/thumbs/"):
				strip(r, "/thumbs") // 404 根因：handler 期望去除前缀后的相对路径
				thumbs.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/imgs/"):
				strip(r, "/imgs")
				imgs.ServeHTTP(w, r)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}
