package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/service"
	"github.com/vcs-sms/shared/response"
)

// ExportHandler handles export-related HTTP requests.
type ExportHandler struct {
	service service.ExportService
}

// NewExportHandler creates a new ExportHandler.
func NewExportHandler(svc service.ExportService) *ExportHandler {
	return &ExportHandler{service: svc}
}

// ExportServers handles POST /api/v1/servers/export — download servers as .xlsx.
func (h *ExportHandler) ExportServers(c *gin.Context) {
	var req dto.ExportRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	filter := req.ToFilter()

	buf, filename, err := h.service.ExportServers(c.Request.Context(), &filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	// Stream Excel file as download
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Length", strconv.Itoa(buf.Len()))
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}
