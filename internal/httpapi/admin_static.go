package httpapi

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/admin/*
var adminStaticEmbed embed.FS

const adminCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func (a *API) registerAdminStatic(mux *http.ServeMux) {
	sub, err := fs.Sub(adminStaticEmbed, "static/admin")
	if err != nil {
		panic("admin static: " + err.Error())
	}

	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})
	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		serveAdminFile(w, r, sub, "index.html")
	})
	mux.HandleFunc("GET /admin/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.PathValue("name"))
		if name == "." || name == ".." || name == "/" || name == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
			return
		}
		serveAdminFile(w, r, sub, name)
	})
}

func serveAdminFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	w.Header().Set("Content-Security-Policy", adminCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")

	f, err := fsys.Open(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}

	ctype := contentTypeForAdmin(name)
	w.Header().Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func contentTypeForAdmin(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
