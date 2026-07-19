package httpapi

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/admin/* static/join/* static/reset-password/*
var publicStaticEmbed embed.FS

const publicPageCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func (a *API) registerPublicStatic(mux *http.ServeMux) {
	registerStaticSite(mux, "admin", "static/admin")
	registerStaticSite(mux, "join", "static/join")
	registerStaticSite(mux, "reset-password", "static/reset-password")
}

func registerStaticSite(mux *http.ServeMux, urlBase, embedDir string) {
	sub, err := fs.Sub(publicStaticEmbed, embedDir)
	if err != nil {
		panic(urlBase + " static: " + err.Error())
	}
	prefix := "/" + urlBase
	mux.HandleFunc("GET "+prefix, func(w http.ResponseWriter, r *http.Request) {
		target := prefix + "/"
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
	mux.HandleFunc("GET "+prefix+"/{$}", func(w http.ResponseWriter, r *http.Request) {
		servePublicStaticFile(w, r, sub, "index.html")
	})
	mux.HandleFunc("GET "+prefix+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.PathValue("name"))
		if name == "." || name == ".." || name == "/" || name == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
			return
		}
		servePublicStaticFile(w, r, sub, name)
	})
}

func servePublicStaticFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	w.Header().Set("Content-Security-Policy", publicPageCSP)
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

	w.Header().Set("Content-Type", contentTypeForStatic(name))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func contentTypeForStatic(name string) string {
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
