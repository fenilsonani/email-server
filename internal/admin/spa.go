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
// For dynamic routes (e.g. /users/123/), it tries the _ placeholder directory.
// For all other requests, it falls back to index.html (SPA routing).
func (s *Server) serveSPA() http.Handler {
	sub, err := fs.Sub(dashboardFS, "dashboard")
	if err != nil {
		s.logger.Error("Failed to create sub FS for dashboard", "error", err.Error())
		return http.NotFoundHandler()
	}

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

		// Try exact file (e.g. _next/static/chunks/foo.js)
		if data, err := fs.ReadFile(sub, filePath); err == nil {
			serveFileData(w, r, filePath, data)
			return
		}

		// Try path/index.html (e.g. login/ → login/index.html)
		indexPath := strings.TrimSuffix(filePath, "/") + "/index.html"
		if data, err := fs.ReadFile(sub, indexPath); err == nil {
			serveFileData(w, r, indexPath, data)
			return
		}

		// Try dynamic route placeholder: replace each path segment with _
		// e.g. users/123 → users/_/index.html
		//      lists/5/members → lists/_/members/index.html
		if dynPath := findDynamicRoute(sub, filePath); dynPath != "" {
			if data, err := fs.ReadFile(sub, dynPath); err == nil {
				serveFileData(w, r, dynPath, data)
				return
			}
		}

		// SPA fallback: serve root index.html
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeContent(w, r, "index.html", epoch, bytes.NewReader(indexHTML))
	})
}

// findDynamicRoute tries replacing path segments with _ to match Next.js
// dynamic route placeholders (generateStaticParams with { id: "_" }).
// For example: "users/123" → "users/_/index.html"
//              "lists/5/members" → "lists/_/members/index.html"
func findDynamicRoute(fsys fs.FS, filePath string) string {
	parts := strings.Split(strings.TrimSuffix(filePath, "/"), "/")

	// Try replacing each segment with _ (from left to right)
	for i := range parts {
		candidate := make([]string, len(parts))
		copy(candidate, parts)
		candidate[i] = "_"
		tryPath := strings.Join(candidate, "/") + "/index.html"
		if _, err := fs.Stat(fsys, tryPath); err == nil {
			return tryPath
		}
	}

	// Try replacing multiple segments (e.g. lists/5/members/3)
	for i := range parts {
		for j := i + 2; j < len(parts); j += 2 {
			candidate := make([]string, len(parts))
			copy(candidate, parts)
			candidate[i] = "_"
			candidate[j] = "_"
			tryPath := strings.Join(candidate, "/") + "/index.html"
			if _, err := fs.Stat(fsys, tryPath); err == nil {
				return tryPath
			}
		}
	}

	return ""
}

// serveFileData serves file bytes with proper Content-Type, Content-Length, and caching
func serveFileData(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if isStaticAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	http.ServeContent(w, r, name, time.Unix(0, 0), bytes.NewReader(data))
}

// isStaticAsset returns true for files that can be aggressively cached
func isStaticAsset(p string) bool {
	ext := path.Ext(p)
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return true
	}
	if strings.Contains(p, "_next/static/") {
		return true
	}
	return false
}
