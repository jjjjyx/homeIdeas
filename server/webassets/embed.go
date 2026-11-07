package webassets

import (
	"embed"
	"io/fs"
)

// 管理后台的构建产物（admin/ 目录 vite build 输出到这里）
//
//go:embed all:dist
var distFS embed.FS

// Dist 返回静态文件根目录，供 http.FileServer 使用。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}
