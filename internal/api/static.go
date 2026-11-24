package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// webDist embeds the built frontend for single-origin deployments (one URL
// serves both the UI and /api/*, so session cookies stay same-origin).
// Checkouts carry only a placeholder; the Docker build copies the real
// `vite build` output into webdist/ before compiling.
//
//go:embed all:webdist
var webDist embed.FS

// registerStaticRoutes serves the embedded frontend with an SPA fallback:
// unknown non-API paths return index.html so refreshes on /bounties/123 keep
// working. API, health, and metrics paths keep their JSON 404s. fsys is a
// parameter so tests can inject fixtures instead of the embedded build.
func registerStaticRoutes(router *gin.Engine, fsys fs.FS) {
	fileServer := http.FileServer(http.FS(fsys))
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || path == "/metrics" {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "not found"}})
			return
		}
		if path != "/" {
			if info, err := fs.Stat(fsys, strings.TrimPrefix(path, "/")); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "not found"}})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}
