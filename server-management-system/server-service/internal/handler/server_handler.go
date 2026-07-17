package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/service"
	ipvalidator "github.com/vcs-sms/server-service/internal/validator"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

// ServerHandler handles HTTP requests for server management endpoints.
type ServerHandler struct {
	svc service.ServerService
}

// NewServerHandler creates a new ServerHandler.
func NewServerHandler(svc service.ServerService) *ServerHandler {
	return &ServerHandler{svc: svc}
}

// CreateServer handles POST /servers
// @Summary Create a new server
// @Tags Servers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body dto.CreateServerRequest true "Server details"
// @Success 201 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 409 {object} response.ApiErrorResponse
// @Router /api/v1/servers [post]
func (h *ServerHandler) CreateServer(c *gin.Context) {
	var req dto.CreateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.CreateServer(c.Request.Context(), &req)
	if err != nil {
		handleServerError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, "Server created successfully", result)
}

// GetServer handles GET /servers/:server_id
// @Summary Get a server by ID
// @Tags Servers
// @Produce json
// @Security BearerAuth
// @Param server_id path string true "Server ID"
// @Success 200 {object} response.ApiResponse
// @Failure 404 {object} response.ApiErrorResponse
// @Router /api/v1/servers/{server_id} [get]
func (h *ServerHandler) GetServer(c *gin.Context) {
	serverID := c.Param("server_id")
	if serverID == "" {
		response.Error(c, http.StatusBadRequest, "Missing server_id parameter")
		return
	}

	result, err := h.svc.GetServer(c.Request.Context(), serverID)
	if err != nil {
		handleServerError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Server retrieved", result)
}

// ListServers handles GET /servers
// @Summary List servers with filtering and pagination
// @Tags Servers
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status"
// @Param server_id query string false "Filter by server ID (partial match)"
// @Param server_name query string false "Filter by server name (partial match)"
// @Param ipv4 query string false "Filter by IPv4 prefix"
// @Param os query string false "Filter by OS"
// @Param location query string false "Filter by location"
// @Param sort_by query string false "Sort field" Enums(server_id, server_name, status, ipv4, created_at, updated_at)
// @Param sort_order query string false "Sort order" Enums(asc, desc)
// @Param page query int false "Page number"
// @Param page_size query int false "Items per page"
// @Success 200 {object} response.ApiResponse
// @Router /api/v1/servers [get]
func (h *ServerHandler) ListServers(c *gin.Context) {
	var filter dto.ServerFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		response.ValidationError(c, "Invalid query parameters", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.ListServers(c.Request.Context(), &filter)
	if err != nil {
		handleServerError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Servers retrieved", result)
}

// UpdateServer handles PUT /servers/:server_id
// @Summary Update an existing server
// @Tags Servers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param server_id path string true "Server ID"
// @Param request body dto.UpdateServerRequest true "Fields to update"
// @Success 200 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 404 {object} response.ApiErrorResponse
// @Router /api/v1/servers/{server_id} [put]
func (h *ServerHandler) UpdateServer(c *gin.Context) {
	serverID := c.Param("server_id")
	if serverID == "" {
		response.Error(c, http.StatusBadRequest, "Missing server_id parameter")
		return
	}

	var req dto.UpdateServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.UpdateServer(c.Request.Context(), serverID, &req)
	if err != nil {
		handleServerError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Server updated successfully", result)
}

// DeleteServer handles DELETE /servers/:server_id
// @Summary Delete a server
// @Tags Servers
// @Produce json
// @Security BearerAuth
// @Param server_id path string true "Server ID"
// @Success 200 {object} response.ApiResponse
// @Failure 404 {object} response.ApiErrorResponse
// @Router /api/v1/servers/{server_id} [delete]
func (h *ServerHandler) DeleteServer(c *gin.Context) {
	serverID := c.Param("server_id")
	if serverID == "" {
		response.Error(c, http.StatusBadRequest, "Missing server_id parameter")
		return
	}

	if err := h.svc.DeleteServer(c.Request.Context(), serverID); err != nil {
		handleServerError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Server deleted successfully", nil)
}

// GetStats handles GET /servers/stats
// @Summary Current status breakdown for the dashboard
// @Tags Servers
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.ApiResponse
// @Router /api/v1/servers/stats [get]
func (h *ServerHandler) GetStats(c *gin.Context) {
	result, err := h.svc.GetStats(c.Request.Context())
	if err != nil {
		handleServerError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Stats retrieved", result)
}

// handleServerError maps service errors to HTTP error responses using errors.Is.
func handleServerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDuplicateServerID) || errors.Is(err, service.ErrDuplicateServerName):
		response.Conflict(c, apperrors.ErrConflict.Message)
	case errors.Is(err, service.ErrServerNotFound):
		response.NotFound(c, apperrors.ErrNotFound.Message)
	case errors.Is(err, ipvalidator.ErrIPNotAllowed):
		response.ErrorWithCode(c, http.StatusUnprocessableEntity,
			apperrors.CodeServerIPNotAllowed, "IPv4 is not an allowed monitoring target")
	default:
		response.InternalError(c, apperrors.ErrInternalServer.Message)
	}
}

// parseValidationErrors converts Gin validation errors to field-level FieldError slice.
func parseValidationErrors(err error) []response.FieldError {
	var fieldErrors []response.FieldError

	if ve, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range ve {
			fieldErrors = append(fieldErrors, response.FieldError{
				Field:   fe.Field(),
				Code:    fe.Tag(),
				Message: fmt.Sprintf("validation failed on '%s'", fe.Tag()),
			})
		}
		return fieldErrors
	}

	// Fallback for non-validation errors
	return []response.FieldError{
		{Field: "body", Code: "VALIDATION", Message: err.Error()},
	}
}
