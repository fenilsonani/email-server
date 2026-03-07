package admin

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed dashboard
var dashboardFS embed.FS

// serveSPA returns an http.Handler that serves the Next.js static export.
// It expects the /admin prefix to already be stripped (via http.StripPrefix).
// For requests matching a real file, it serves the file directly.
// For all other requests, it falls back to index.html (SPA routing).
func (s *Server) serveSPA() http.Handler {
	// Strip the "dashboard" prefix from the embed FS
	sub, err := fs.Sub(dashboardFS, "dashboard")
	if err != nil {
		s.logger.Error("Failed to create sub FS for dashboard", "error", err.Error())
		return http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := r.URL.Path
		if urlPath == "" || urlPath == "/" {
			urlPath = "/index.html"
		}

		// Don't serve SPA for API routes
		if strings.HasPrefix(urlPath, "/api/") {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean(urlPath)
		filePath := strings.TrimPrefix(cleanPath, "/")

		// Try exact file
		if f, err := sub.Open(filePath); err == nil {
			defer f.Close()
			serveFile(w, filePath, f)
			return
		}

		// Try path/index.html (for trailing slash routes like /login/)
		indexPath := strings.TrimSuffix(filePath, "/") + "/index.html"
		if f, err := sub.Open(indexPath); err == nil {
			defer f.Close()
			serveFile(w, indexPath, f)
			return
		}

		// SPA fallback: serve root index.html for client-side routing
		if f, err := sub.Open("index.html"); err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			io.Copy(w, f.(io.Reader))
			return
		}

		http.NotFound(w, r)
	})
}

// serveFile writes a file from the embedded FS directly to the response
func serveFile(w http.ResponseWriter, name string, f fs.File) {
	// Set content type from extension
	ext := path.Ext(name)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)

	// Cache static assets aggressively, HTML pages not at all
	if isStaticAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	io.Copy(w, f.(io.Reader))
}

// isStaticAsset returns true for files that can be aggressively cached
func isStaticAsset(p string) bool {
	ext := path.Ext(p)
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return true
	}
	return false
}
