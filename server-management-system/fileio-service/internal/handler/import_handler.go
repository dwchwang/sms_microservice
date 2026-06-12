package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/fileio-service/internal/service"
	"github.com/vcs-sms/shared/response"
)

// ImportHandler handles import-related HTTP requests.
type ImportHandler struct {
	service service.ImportService
}

// NewImportHandler creates a new ImportHandler.
func NewImportHandler(svc service.ImportService) *ImportHandler {
	return &ImportHandler{service: svc}
}

// ImportServers handles POST /api/v1/servers/import — upload .xlsx for import.
func (h *ImportHandler) ImportServers(c *gin.Context) {
	// 1. Get uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "File is required")
		return
	}
	defer file.Close()

	// 2. Get user_id from header (injected by API Gateway)
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		userID = c.GetHeader("X-Auth-User-ID")
	}

	// 3. Call service
	result, err := h.service.InitiateImport(c.Request.Context(), file, header, userID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// 4. Return 202 Accepted
	response.Success(c, http.StatusAccepted, "Import job created", result)
}

// GetImportStatus handles GET /api/v1/servers/import/:job_id — check import progress.
func (h *ImportHandler) GetImportStatus(c *gin.Context) {
	jobID := c.Param("job_id")
	if jobID == "" {
		response.Error(c, http.StatusBadRequest, "job_id is required")
		return
	}

	result, err := h.service.GetImportJobStatus(c.Request.Context(), jobID)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, http.StatusOK, "Import job status", result)
}
