package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/repository"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

const (
	defaultPopulationLimit = 1000
	maxPopulationLimit     = 1000
)

// InternalHandler serves routes that are not published through the gateway.
type InternalHandler struct {
	repo repository.ServerRepository
}

// NewInternalHandler creates an InternalHandler.
func NewInternalHandler(repo repository.ServerRepository) *InternalHandler {
	return &InternalHandler{repo: repo}
}

// ListPopulation handles GET /internal/servers. Reporting calls it once a day
// to learn which servers existed during a window, including deleted ones, so a
// server that was never pinged still shows up in the report.
func (h *InternalHandler) ListPopulation(c *gin.Context) {
	// Both are required: defaulting created_before to the zero time would match
	// no rows and return an empty population instead of an error.
	createdBefore, err := time.Parse(time.RFC3339, c.Query("created_before"))
	if err != nil {
		response.ValidationError(c, "created_before is required and must be RFC3339")
		return
	}
	deletedAfter, err := time.Parse(time.RFC3339, c.Query("deleted_after"))
	if err != nil {
		response.ValidationError(c, "deleted_after is required and must be RFC3339")
		return
	}

	limit := defaultPopulationLimit
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			response.ValidationError(c, "Invalid limit")
			return
		}
		limit = min(limit, maxPopulationLimit)
	}

	servers, err := h.repo.FindPopulation(c.Request.Context(), repository.PopulationQuery{
		CreatedBefore: createdBefore,
		DeletedAfter:  deletedAfter,
		Cursor:        c.Query("cursor"),
		Limit:         limit,
	})
	if err != nil {
		response.InternalError(c, apperrors.ErrInternalServer.Message)
		return
	}

	out := make([]dto.PopulationServer, 0, len(servers))
	for _, s := range servers {
		item := dto.PopulationServer{
			ServerID:   s.ServerID,
			ServerName: s.ServerName,
			CreatedAt:  s.CreatedAt,
		}
		if s.DeletedAt.Valid {
			deleted := s.DeletedAt.Time
			item.DeletedAt = &deleted
		}
		out = append(out, item)
	}

	// A short page is the last page, so the cursor empties and the caller stops.
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].ServerID
	}

	response.Success(c, http.StatusOK, "Population retrieved", dto.PopulationResponse{
		Servers:    out,
		NextCursor: next,
	})
}
