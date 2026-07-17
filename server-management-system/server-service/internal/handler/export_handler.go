package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/service"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ExportHandler handles Excel export.
type ExportHandler struct {
	svc service.ExportService
}

// NewExportHandler creates an ExportHandler.
func NewExportHandler(svc service.ExportService) *ExportHandler {
	return &ExportHandler{svc: svc}
}

// ExportServers handles POST /servers/export. It is a read, but uses POST so a
// long filter cannot overflow the query string.
// @Summary Export servers to an Excel file
// @Tags Servers
// @Accept json
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Param request body dto.ServerFilter false "Filter and sort"
// @Success 200 {file} binary
// @Router /api/v1/servers/export [post]
func (h *ExportHandler) ExportServers(c *gin.Context) {
	var filter dto.ServerFilter
	// An empty body means export everything.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&filter); err != nil {
			response.ValidationError(c, "Invalid filter", parseValidationErrors(err)...)
			return
		}
	}

	buf, filename, err := h.svc.Export(c.Request.Context(), &filter)
	if err != nil {
		response.InternalError(c, apperrors.ErrInternalServer.Message)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, xlsxContentType, buf.Bytes())
}
