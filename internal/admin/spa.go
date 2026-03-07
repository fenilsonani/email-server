package admin

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dashboard
var dashboardFS embed.FS

// serveSPA returns an http.Handler that serves the Next.js static export.
// For requests matching a real file, it serves the file directly.
// For all other requests, it falls back to index.html (SPA routing).
func (s *Server) serveSPA() http.Handler {
	// Strip the "dashboard" prefix from the embed FS
	sub, err := fs.Sub(dashboardFS, "dashboard")
	if err != nil {
		s.logger.Error("Failed to create sub FS for dashboard", "error", err.Error())
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip /admin prefix to get the path within the SPA
		urlPath := strings.TrimPrefix(r.URL.Path, "/admin")
		if urlPath == "" {
			urlPath = "/"
		}

		// Don't serve SPA for API routes
		if strings.HasPrefix(urlPath, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the exact file first
		cleanPath := path.Clean(urlPath)
		if cleanPath == "/" {
			cleanPath = "/index.html"
		}

		// Check if the file exists in the embedded FS
		filePath := strings.TrimPrefix(cleanPath, "/")

		// Try exact file
		if f, err := sub.Open(filePath); err == nil {
			f.Close()
			// Serve static assets with cache headers
			if isStaticAsset(filePath) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			r.URL.Path = urlPath
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try path/index.html (for trailing slash routes)
		indexPath := strings.TrimSuffix(filePath, "/") + "/index.html"
		if f, err := sub.Open(indexPath); err == nil {
			f.Close()
			r.URL.Path = "/" + indexPath
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve root index.html for client-side routing
		// The Next.js client-side router will pick up the URL and render correctly
		r.URL.Path = "/index.html"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	})
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
