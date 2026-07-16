package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Meta represents metadata for API responses
type Meta struct {
	RequestID  string `json:"request_id"`
	Timestamp  string `json:"timestamp"`
	DurationMs *int64 `json:"duration_ms,omitempty"`
}

// ApiResponse represents a success response
type ApiResponse struct {
	Status  string      `json:"status"`         // "success"
	Code    int         `json:"code"`           // HTTP status code
	Message string      `json:"message"`        // human-readable message
	Data    interface{} `json:"data,omitempty"` // response payload
	Meta    *Meta       `json:"meta,omitempty"` // metadata
}

// ApiErrorResponse represents an error response
type ApiErrorResponse struct {
	Status  string       `json:"status"`           // "error"
	Code    string       `json:"code"`             // error code string
	Message string       `json:"message"`          // human-readable message
	Errors  []FieldError `json:"errors,omitempty"` // field-level errors (for validation)
	Meta    *Meta        `json:"meta,omitempty"`   // metadata
}

// FieldError represents a single field validation error
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// buildMeta extracts metadata from the gin context
func buildMeta(c *gin.Context) *Meta {
	requestID := ""
	if rid, exists := c.Get("request_id"); exists {
		requestID, _ = rid.(string)
	}
	return &Meta{
		RequestID: requestID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// Success sends a success response
func Success(c *gin.Context, httpCode int, message string, data interface{}) {
	c.JSON(httpCode, ApiResponse{
		Status:  "success",
		Code:    httpCode,
		Message: message,
		Data:    data,
		Meta:    buildMeta(c),
	})
}

// ErrorWithCode sends an error response with string error code
func ErrorWithCode(c *gin.Context, httpCode int, errorCode string, message string, fieldErrors ...FieldError) {
	c.AbortWithStatusJSON(httpCode, ApiErrorResponse{
		Status:  "error",
		Code:    errorCode,
		Message: message,
		Errors:  fieldErrors,
		Meta:    buildMeta(c),
	})
}

// --- Backward compatibility methods ---

// Error sends an error response with generic string code
func Error(c *gin.Context, httpCode int, message string, fieldErrors ...FieldError) {
	ErrorWithCode(c, httpCode, "COMMON_ERROR", message, fieldErrors...)
}

// ValidationError sends a 422 validation error response
func ValidationError(c *gin.Context, message string, fieldErrors ...FieldError) {
	ErrorWithCode(c, http.StatusUnprocessableEntity, "COMMON_VALIDATION_FAILED", message, fieldErrors...)
}

// NotFound sends a 404 error response
func NotFound(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusNotFound, "COMMON_NOT_FOUND", message)
}

// Unauthorized sends a 401 error response
func Unauthorized(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusUnauthorized, "COMMON_UNAUTHORIZED", message)
}

// Forbidden sends a 403 error response
func Forbidden(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusForbidden, "COMMON_FORBIDDEN_SCOPE", message)
}

// Conflict sends a 409 error response
func Conflict(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusConflict, "COMMON_CONFLICT", message)
}

// InternalError sends a 500 error response
func InternalError(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusInternalServerError, "COMMON_INTERNAL_ERROR", message)
}
