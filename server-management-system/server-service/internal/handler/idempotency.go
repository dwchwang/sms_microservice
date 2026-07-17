package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

const (
	idempotencyHeader = "Idempotency-Key"
	actorHeader       = "X-User-Id"
	idempotencyTTL    = 24 * time.Hour
)

// bodyCapture tees the handler's response so it can be stored and replayed.
type bodyCapture struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyCapture) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyCapture) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Idempotency replays the stored response when the same key and body arrive
// twice, and rejects the same key carrying a different body.
func Idempotency(repo repository.IdempotencyRepository, log zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader(idempotencyHeader)
		if key == "" {
			response.ErrorWithCode(c, http.StatusBadRequest,
				apperrors.CodeValidationFailed, "Idempotency-Key header is required")
			return
		}

		actor := c.GetHeader(actorHeader)
		if actor == "" {
			actor = "anonymous"
		}
		endpoint := c.FullPath()

		hash, err := hashBody(c)
		if err != nil {
			response.InternalError(c, apperrors.ErrInternalServer.Message)
			return
		}

		rec := &model.Idempotency{
			ActorID:        actor,
			Endpoint:       endpoint,
			IdempotencyKey: key,
			RequestHash:    hash,
			State:          model.IdempotencyProcessing,
			ExpiresAt:      time.Now().UTC().Add(idempotencyTTL),
		}

		claimed, existing, err := repo.Claim(c.Request.Context(), rec)
		if err != nil {
			log.Error().Err(err).Msg("Failed to claim idempotency key")
			response.InternalError(c, apperrors.ErrInternalServer.Message)
			return
		}
		if !claimed {
			replay(c, existing, hash)
			return
		}

		capture := &bodyCapture{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = capture

		c.Next()

		status := c.Writer.Status()
		if status >= 200 && status < 300 {
			rec.StatusCode = status
			rec.ResponseBody = capture.body.Bytes()
			if err := repo.Complete(c.Request.Context(), rec); err != nil {
				log.Error().Err(err).Msg("Failed to store idempotent response")
			}
			return
		}

		// A failed attempt must not lock the key: the caller may fix the input
		// and retry with the same key.
		if err := repo.Release(c.Request.Context(), actor, endpoint, key); err != nil {
			log.Error().Err(err).Msg("Failed to release idempotency key")
		}
	}
}

// replay returns the stored outcome, or 409 when the key is reused for
// different content or is still in flight.
func replay(c *gin.Context, existing *model.Idempotency, hash string) {
	if existing == nil {
		response.Conflict(c, "Request is already in progress")
		return
	}
	if existing.RequestHash != hash {
		response.Conflict(c, "Idempotency-Key was already used with a different request body")
		return
	}
	if existing.State != model.IdempotencyCompleted {
		response.Conflict(c, "Request is already in progress")
		return
	}

	c.Data(existing.StatusCode, "application/json", existing.ResponseBody)
	c.Abort()
}

// hashBody reads the body to hash it, then restores it for the handler.
func hashBody(c *gin.Context) (string, error) {
	if c.Request.Body == nil {
		return hashOf(nil), nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return hashOf(body), nil
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
