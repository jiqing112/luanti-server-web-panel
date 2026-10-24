// Package spa: 嵌入式 SPA 静态资源服务（未知路径回退 index.html）。
package spa

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler 服务静态文件；未命中时直接返回 index.html 内容。
// 注意：不能用 http.FileServer 做回退——它会把 /index.html 规范化重定向到
// ./，与 SPA 回退形成 30x 循环。
func Handler(fsys fs.FS, index string) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = index
		}
		if p == index {
			// 入口 HTML 禁止缓存：保证发版后浏览器立即取到新资产引用
			w.Header().Set("Cache-Control", "no-store")
		}
		if _, err := fs.Stat(fsys, p); err != nil {
			// SPA 路由回退：直接返回 index.html 内容
			data, err := fs.ReadFile(fsys, index)
			if err != nil {
				http.Error(w, "index.html missing (run: cd web && npm run build)", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
