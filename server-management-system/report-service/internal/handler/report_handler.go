package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/infrastructure/email"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/service"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
	"gorm.io/gorm"
)

// ReportHandler serves the reporting endpoints.
type ReportHandler struct {
	reports service.ReportService
	sends   service.SendService
}

// NewReportHandler creates a ReportHandler.
func NewReportHandler(reports service.ReportService, sends service.SendService) *ReportHandler {
	return &ReportHandler{reports: reports, sends: sends}
}

// GetSummary handles GET /reports/summary
// @Summary Uptime report for a date range
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Param start_date query string true "YYYY-MM-DD"
// @Param end_date query string true "YYYY-MM-DD"
// @Success 200 {object} response.ApiResponse
// @Router /api/v1/reports/summary [get]
func (h *ReportHandler) GetSummary(c *gin.Context) {
	var req dto.SummaryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ValidationError(c, "start_date and end_date are required")
		return
	}

	summary, err := h.reports.Summary(c.Request.Context(), req.StartDate, req.EndDate)
	if err != nil {
		handleReportError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Report generated", summary)
}

// SendReport handles POST /reports
// @Summary Generate a report and email it
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.SendReportRequest true "Report range and recipient"
// @Success 200 {object} response.ApiResponse
// @Router /api/v1/reports [post]
func (h *ReportHandler) SendReport(c *gin.Context) {
	var req dto.SendReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body")
		return
	}

	job, err := h.sends.Send(c.Request.Context(), req,
		model.TypeOnDemand, c.GetHeader("X-User-Id"), c.GetHeader("Idempotency-Key"))
	if err != nil {
		handleReportError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Report sent", job)
}

// GetReport handles GET /reports/:id
// @Summary Fetch a report job
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Param id path string true "Job ID"
// @Success 200 {object} response.ApiResponse
// @Router /api/v1/reports/{id} [get]
func (h *ReportHandler) GetReport(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.ValidationError(c, "Invalid report id")
		return
	}

	job, err := h.sends.Get(c.Request.Context(), id)
	if err != nil {
		handleReportError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Report retrieved", job)
}

// handleReportError maps service errors onto the shared error contract.
func handleReportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidRange):
		response.ErrorWithCode(c, http.StatusUnprocessableEntity,
			apperrors.CodeReportInvalidRange, err.Error())
	case errors.Is(err, service.ErrDataUnavailable):
		// Naming the missing days is the point: a report over a hole would be
		// wrong rather than merely unavailable.
		response.ErrorWithCode(c, http.StatusUnprocessableEntity,
			apperrors.CodeReportDataUnavailable, err.Error())
	case errors.Is(err, service.ErrIdempotencyConflict):
		response.ErrorWithCode(c, http.StatusConflict,
			apperrors.CodeReportIdempotency, err.Error())
	case errors.Is(err, email.ErrRecipientNotAllowed):
		response.ErrorWithCode(c, http.StatusUnprocessableEntity,
			apperrors.CodeReportRecipientBlocked, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.NotFound(c, apperrors.ErrNotFound.Message)
	default:
		response.InternalError(c, apperrors.ErrInternalServer.Message)
	}
}
