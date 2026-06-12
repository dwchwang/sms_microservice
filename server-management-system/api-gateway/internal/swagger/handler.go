package swagger

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>VCS-SMS API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>body{margin:0}#swagger-ui{min-height:100vh}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/swagger/doc.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        persistAuthorization: true
      });
    };
  </script>
</body>
</html>`

// RegisterRoutes exposes the OpenAPI document and Swagger UI through the gateway.
func RegisterRoutes(r *gin.Engine, specPath string) {
	r.GET("/swagger", serveIndex)
	r.GET("/swagger/", serveIndex)
	r.GET("/swagger/index.html", serveIndex)
	r.GET("/swagger/doc.yaml", serveSpec(specPath))
}

func serveIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(indexHTML))
}

func serveSpec(specPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := specPath
		if path == "" {
			path = resolveSpecPath()
		}
		if !fileExists(path) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"code":    http.StatusInternalServerError,
				"message": "OpenAPI spec not found",
			})
			return
		}
		c.File(path)
	}
}

func resolveSpecPath() string {
	if path := os.Getenv("API_SPEC_PATH"); fileExists(path) {
		return path
	}

	for _, path := range []string{
		"/app/docs/api-spec.yaml",
		"docs/api-spec.yaml",
		"../docs/api-spec.yaml",
	} {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
