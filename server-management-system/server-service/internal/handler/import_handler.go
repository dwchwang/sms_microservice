package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/server-service/internal/service"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

// MaxImportBytes caps the upload before it is parsed.
const MaxImportBytes = 20 << 20

// ImportHandler handles Excel import.
type ImportHandler struct {
	svc service.ImportService
}

// NewImportHandler creates an ImportHandler.
func NewImportHandler(svc service.ImportService) *ImportHandler {
	return &ImportHandler{svc: svc}
}

// ImportServers handles POST /servers/import
// @Summary Import servers from an Excel file
// @Tags Servers
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "xlsx file"
// @Success 200 {object} response.ApiResponse
// @Failure 422 {object} response.ApiErrorResponse
// @Router /api/v1/servers/import [post]
func (h *ImportHandler) ImportServers(c *gin.Context) {
	started := time.Now()

	header, err := c.FormFile("file")
	if err != nil {
		response.ErrorWithCode(c, http.StatusBadRequest,
			apperrors.CodeServerImportRejected, "Missing file upload")
		return
	}
	if header.Size > MaxImportBytes {
		response.ErrorWithCode(c, http.StatusUnprocessableEntity,
			apperrors.CodeServerImportRejected, "File exceeds the maximum size")
		return
	}

	file, err := header.Open()
	if err != nil {
		response.InternalError(c, apperrors.ErrInternalServer.Message)
		return
	}
	defer file.Close()

	result, err := h.svc.Import(c.Request.Context(), file)
	if err != nil {
		if errors.Is(err, service.ErrImportFileRejected) {
			response.ErrorWithCode(c, http.StatusUnprocessableEntity,
				apperrors.CodeServerImportRejected, err.Error())
			return
		}
		response.InternalError(c, apperrors.ErrInternalServer.Message)
		return
	}

	respondWithDuration(c, "Import completed", result, started)
}

// respondWithDuration adds meta.duration_ms so import cost is measurable in production.
func respondWithDuration(c *gin.Context, message string, data any, started time.Time) {
	ms := time.Since(started).Milliseconds()
	c.JSON(http.StatusOK, response.ApiResponse{
		Status:  "success",
		Code:    http.StatusOK,
		Message: message,
		Data:    data,
		Meta: &response.Meta{
			RequestID:  requestIDOf(c),
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			DurationMs: &ms,
		},
	})
}

func requestIDOf(c *gin.Context) string {
	if rid, ok := c.Get("request_id"); ok {
		if s, ok := rid.(string); ok {
			return s
		}
	}
	return ""
}
