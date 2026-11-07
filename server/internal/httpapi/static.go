package httpapi

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"family-ledger/server/webassets"
)

// adminStatic 提供管理后台的单页应用：文件存在就返回文件，否则回落到 index.html。
func adminStatic() http.Handler {
	root := webassets.Dist()
	if root == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "管理后台未构建", http.StatusServiceUnavailable)
		})
	}
	// FileServer 以 r.URL.Path 为相对路径查文件，必须先把 /admin/ 前缀剥掉
	fileServer := http.StripPrefix("/admin/", http.FileServer(http.FS(root)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/admin")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			serveIndex(w, root)
			return
		}
		if f, err := root.Open(path.Clean(rel)); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// 前端路由（/admin/users 之类）回落到 index.html
		serveIndex(w, root)
	})
}

func serveIndex(w http.ResponseWriter, root fs.FS) {
	f, err := root.Open("index.html")
	if err != nil {
		http.Error(w, "index.html 缺失", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}
