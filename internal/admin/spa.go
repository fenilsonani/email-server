package admin

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dashboard
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

	// Pre-read index.html for SPA fallback
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		s.logger.Error("Failed to read index.html from dashboard", "error", err.Error())
		return http.NotFoundHandler()
	}

	epoch := time.Unix(0, 0)

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
		if data, err := fs.ReadFile(sub, filePath); err == nil {
			serveFileData(w, r, filePath, data)
			return
		}

		// Try path/index.html (for trailing slash routes like /login/)
		indexPath := strings.TrimSuffix(filePath, "/") + "/index.html"
		if data, err := fs.ReadFile(sub, indexPath); err == nil {
			serveFileData(w, r, indexPath, data)
			return
		}

		// SPA fallback: serve root index.html for client-side routing
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeContent(w, r, "index.html", epoch, bytes.NewReader(indexHTML))
	})
}

// serveFileData serves file bytes with proper Content-Type, Content-Length, and caching
func serveFileData(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	// Cache static assets aggressively, HTML pages not at all
	if isStaticAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	// http.ServeContent sets Content-Type from name, Content-Length, handles Range, etc.
	http.ServeContent(w, r, name, time.Unix(0, 0), bytes.NewReader(data))
}

// isStaticAsset returns true for files that can be aggressively cached
func isStaticAsset(p string) bool {
	ext := path.Ext(p)
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return true
	}
	// Also catch hashed Next.js chunks in _next/static/
	if strings.Contains(p, "_next/static/") {
		return true
	}
	return false
}

