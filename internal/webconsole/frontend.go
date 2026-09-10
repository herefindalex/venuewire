package webconsole

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || s.assets == nil {
		writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found.", requestID(r))
		return
	}
	requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requested == "." || requested == "" {
		requested = "index.html"
	}
	if strings.HasPrefix(requested, ".") || strings.Contains(requested, "/.") {
		writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found.", requestID(r))
		return
	}
	if _, err := fs.Stat(s.assets, requested); err != nil {
		if path.Ext(requested) != "" {
			writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found.", requestID(r))
			return
		}
		requested = "index.html"
	}
	if requested == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		contents, err := fs.ReadFile(s.assets, requested)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "The requested resource was not found.", requestID(r))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(contents)
		return
	} else if strings.HasPrefix(requested, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	r.URL.Path = "/" + requested
	http.FileServer(http.FS(s.assets)).ServeHTTP(w, r)
}
