package console

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/admin/")
		if name == r.URL.Path && r.URL.Path != "/admin" {
			http.NotFound(w, r)
			return
		}
		if name == "" || name == "." {
			name = "index.html"
		}
		name = path.Clean(name)
		if strings.HasPrefix(name, "..") {
			http.NotFound(w, r)
			return
		}
		file, openErr := sub.Open(name)
		if openErr != nil {
			serveIndex(w, r, sub)
			return
		}
		defer file.Close()
		stat, statErr := file.Stat()
		if statErr != nil || stat.IsDir() {
			serveIndex(w, r, sub)
			return
		}
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), file.(interface {
			Read([]byte) (int, error)
			Seek(int64, int) (int64, error)
		}))
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	file, err := sub.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", stat.ModTime(), file.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	}))
}
