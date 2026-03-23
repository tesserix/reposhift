package api

import (
	"embed"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed swagger-ui
var swaggerUI embed.FS

// setupSwaggerUIGin sets up the Swagger UI routes for Gin
func (s *Server) setupSwaggerUIGin(r *gin.Engine) {
	// Serve Swagger UI using Gin's static file server
	r.StaticFS("/swagger", http.FS(swaggerUI))

	// Serve OpenAPI spec
	r.GET("/openapi.yaml", ginAdapter(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		http.ServeFile(w, r, "docs/openapi/swagger.yaml")
	}))

	// Redirect /api-docs to /swagger
	r.GET("/api-docs", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/")
	})
}

// FileServer is a http.Handler that serves static files from the embedded filesystem
type FileServer struct {
	root     embed.FS
	basePath string
}

func (fs *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path to prevent directory traversal
	urlPath := path.Clean(r.URL.Path)

	// Combine with base path
	filePath := path.Join(fs.basePath, urlPath)

	// Try to open the file
	file, err := fs.root.Open(filePath)
	if err != nil {
		// If the file doesn't exist, try to serve index.html
		if strings.HasSuffix(urlPath, "/") {
			indexPath := path.Join(filePath, "index.html")
			indexFile, indexErr := fs.root.Open(indexPath)
			if indexErr == nil {
				defer indexFile.Close()
				http.ServeContent(w, r, "index.html", time.Time{}, indexFile.(io.ReadSeeker))
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	// Serve the file
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If it's a directory, try to serve index.html
	if stat.IsDir() {
		indexPath := path.Join(filePath, "index.html")
		indexFile, err := fs.root.Open(indexPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer indexFile.Close()
		http.ServeContent(w, r, "index.html", time.Time{}, indexFile.(io.ReadSeeker))
		return
	}

	// Serve the file
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file.(io.ReadSeeker))
}
