package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// scalarHTML renders the contract with Scalar API Reference. The script is
// pinned to an exact version.
const scalarHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Field Day API</title>
</head>
<body>
  <script id="api-reference" data-url="/api/openapi.json"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.76.4"></script>
</body>
</html>`

// DocsHandler serves the interactive API reference.
func DocsHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, scalarHTML)
}

// SpecHandler serves the OpenAPI document as JSON.
func SpecHandler(specJSON []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "application/json", specJSON)
	}
}
