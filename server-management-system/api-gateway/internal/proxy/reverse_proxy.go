package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// httpClient is a shared HTTP client for proxying requests.
// Timeout is generous to support large file uploads/downloads.
var httpClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

// NewReverseProxy creates a Gin handler that proxies requests to the target URL.
// It properly forwards the request body (including multipart file uploads) and
// preserves all headers, making it safe for both JSON APIs and file operations.
func NewReverseProxy(target string) gin.HandlerFunc {
	// Validate and parse target URL once at creation time
	remote, err := url.Parse(target)
	if err != nil {
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(502, gin.H{
				"status":  "error",
				"code":    502,
				"message": "Invalid upstream service URL",
			})
		}
	}

	return func(c *gin.Context) {
		proxyRequest(c, remote)
	}
}

// proxyRequest forwards the incoming request to the backend service and writes
// the backend's response back to the client.
func proxyRequest(c *gin.Context, target *url.URL) {
	// 1. Build the target URL by combining the backend base URL with the original path
	targetURL := *target // shallow copy
	targetURL.Path = normalizePath(target.Path, c.Request.URL.Path)
	targetURL.RawQuery = c.Request.URL.RawQuery

	// 2. Create a new request to the backend
	// Read the entire body to ensure it can be forwarded even if partially consumed
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"code":    500,
			"message": "Failed to read request body",
		})
		return
	}
	// Close the original body (best practice)
	c.Request.Body.Close()

	proxyReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		targetURL.String(),
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"code":    500,
			"message": "Failed to create proxy request",
		})
		return
	}

	// 3. Copy headers from the original request
	copyHeaders(c.Request.Header, proxyReq.Header)

	// Remove hop-by-hop headers that shouldn't be forwarded
	proxyReq.Header.Del("Connection")
	proxyReq.Header.Del("Keep-Alive")
	proxyReq.Header.Del("Proxy-Authorization")
	proxyReq.Header.Del("Te")
	proxyReq.Header.Del("Trailer")
	proxyReq.Header.Del("Transfer-Encoding")

	// Set forwarded headers for the backend to know the original client
	proxyReq.Header.Set("X-Forwarded-For", c.ClientIP())
	proxyReq.Header.Set("X-Forwarded-Host", c.Request.Host)
	if c.Request.TLS != nil {
		proxyReq.Header.Set("X-Forwarded-Proto", "https")
	} else {
		proxyReq.Header.Set("X-Forwarded-Proto", "http")
	}

	// Set the Host header to the backend's host
	proxyReq.Host = target.Host

	// 4. Send the request to the backend
	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{
			"status":  "error",
			"code":    502,
			"message": "Failed to connect to backend service",
		})
		return
	}
	defer resp.Body.Close()

	// 5. Copy response headers back to the client
	copyHeaders(resp.Header, c.Writer.Header())

	// Add CORS expose headers for rate limit info
	c.Writer.Header().Set("Access-Control-Expose-Headers",
		"X-Request-ID, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset")

	// 6. Write the response status and body back to the client
	c.Writer.WriteHeader(resp.StatusCode)

	// Stream the response body efficiently
	_, copyErr := io.Copy(c.Writer, resp.Body)
	if copyErr != nil {
		// Response already started writing headers, can't send error status
		// Just log this (caller can add logging if needed)
		return
	}
}

// normalizePath joins the base path and request path, ensuring no double slashes.
func normalizePath(basePath, reqPath string) string {
	// If the target URL already includes a base path (e.g., /api/v1),
	// we join it with the request path.
	basePath = strings.TrimRight(basePath, "/")
	reqPath = "/" + strings.TrimLeft(reqPath, "/")
	return basePath + reqPath
}

// copyHeaders copies all headers from src to dst.
func copyHeaders(src http.Header, dst http.Header) {
	for key, values := range src {
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}
