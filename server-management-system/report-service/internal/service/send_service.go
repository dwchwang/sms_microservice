package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/report-service/internal/dto"
	"github.com/vcs-sms/report-service/internal/infrastructure/email"
	"github.com/vcs-sms/report-service/internal/infrastructure/excel"
	"github.com/vcs-sms/report-service/internal/model"
	"github.com/vcs-sms/report-service/internal/repository"
)

// SendService generates a report and mails it, tracking the outcome.
type SendService interface {
	Send(ctx context.Context, req dto.SendReportRequest, reportType, requesterID, idempotencyKey string) (*dto.JobResponse, error)
	Get(ctx context.Context, id uuid.UUID) (*dto.JobResponse, error)
}

type sendService struct {
	reports  ReportService
	jobs     repository.JobRepository
	sender   email.Sender
	renderer email.Renderer
	gen      excel.Generator
	loc      *time.Location
	log      zerolog.Logger
}

// NewSendService creates a SendService.
func NewSendService(
	reports ReportService,
	jobs repository.JobRepository,
	sender email.Sender,
	renderer email.Renderer,
	gen excel.Generator,
	loc *time.Location,
	log zerolog.Logger,
) SendService {
	return &sendService{
		reports: reports, jobs: jobs, sender: sender,
		renderer: renderer, gen: gen, loc: loc, log: log,
	}
}

// Send walks the job through processing → generated → sending → sent. A job row
// exists before the email is attempted, so an ambiguous send is still on record.
func (s *sendService) Send(ctx context.Context, req dto.SendReportRequest, reportType, requesterID, idempotencyKey string) (*dto.JobResponse, error) {
	start, end, err := s.reports.ParseRange(req.StartDate, req.EndDate, time.Now())
	if err != nil {
		return nil, err
	}

	job := &model.ReportJob{
		ID:             uuid.New(),
		ReportType:     reportType,
		RequesterID:    requesterID,
		IdempotencyKey: idempotencyKey,
		StartAt:        start,
		EndAt:          end,
		RecipientEmail: req.RecipientEmail,
		State:          model.StateProcessing,
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create report job: %w", err)
	}

	summary, err := s.reports.Summary(ctx, req.StartDate, req.EndDate)
	if err != nil {
		_ = s.jobs.SetFailed(ctx, job.ID, model.StateFailed, err.Error(), "")
		return nil, err
	}

	body, _ := json.Marshal(summary)
	if err := s.jobs.SetGenerated(ctx, job.ID, body); err != nil {
		s.log.Error().Err(err).Str("job_id", job.ID.String()).Msg("Failed to store the generated report")
	}

	subject, html, err := s.renderer.Render(summary)
	if err != nil {
		_ = s.jobs.SetFailed(ctx, job.ID, model.StateFailed, err.Error(), "")
		return nil, err
	}

	if err := s.jobs.SetState(ctx, job.ID, model.StateSending); err != nil {
		s.log.Error().Err(err).Str("job_id", job.ID.String()).Msg("Failed to mark the job sending")
	}

	msg := email.Message{To: req.RecipientEmail, Subject: subject, HTML: html}
	// The attachment is supplementary: a failure to build it must not lose the
	// report the body already carries.
	if att, err := s.buildAttachment(ctx, start, end, summary); err != nil {
		s.log.Warn().Err(err).Str("job_id", job.ID.String()).
			Msg("Failed to build uptime attachment; sending without it")
	} else {
		msg.Attachment = att
	}

	messageID, sendErr := s.sender.Send(msg)

	state := s.recordSend(ctx, job.ID, messageID, sendErr)

	resp := jobResponse(job, summary)
	resp.State = state
	resp.SMTPMessageID = messageID
	if sendErr != nil {
		resp.ErrorMessage = sendErr.Error()
		if errors.Is(sendErr, email.ErrRecipientNotAllowed) {
			return nil, sendErr
		}
	}
	return resp, nil
}

// buildAttachment renders the per-server uptime xlsx for the window.
func (s *sendService) buildAttachment(ctx context.Context, start, end time.Time, summary *dto.SummaryResponse) (*email.Attachment, error) {
	rows, err := s.reports.ServerUptimeRows(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to read uptime rows: %w", err)
	}
	buf, err := s.gen.Generate(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to generate xlsx: %w", err)
	}
	return &email.Attachment{
		Filename:    excel.Filename(summary.StartDate, summary.EndDate),
		ContentType: excel.ContentType,
		Content:     buf.Bytes(),
	}, nil
}

// recordSend maps the send outcome onto a state and stores it.
func (s *sendService) recordSend(ctx context.Context, id uuid.UUID, messageID string, sendErr error) string {
	if sendErr == nil {
		if err := s.jobs.SetSent(ctx, id, messageID); err != nil {
			s.log.Error().Err(err).Str("job_id", id.String()).Msg("Failed to mark the job sent")
		}
		return model.StateSent
	}

	// The body was already in flight, so nobody knows whether it arrived. This
	// is recorded rather than retried; a blind retry could send it twice.
	state := model.StateFailed
	if errors.Is(sendErr, email.ErrAmbiguousDelivery) {
		state = model.StateDeliveryUnknown
		s.log.Error().Err(sendErr).Str("job_id", id.String()).Str("message_id", messageID).
			Msg("Delivery outcome unknown — check the Sent folder for this Message-ID before resending")
	} else {
		s.log.Error().Err(sendErr).Str("job_id", id.String()).Msg("Failed to send report")
	}

	if err := s.jobs.SetFailed(ctx, id, state, sendErr.Error(), messageID); err != nil {
		s.log.Error().Err(err).Str("job_id", id.String()).Msg("Failed to record the send failure")
	}
	return state
}

// Get returns a job, decoding the stored summary when there is one.
func (s *sendService) Get(ctx context.Context, id uuid.UUID) (*dto.JobResponse, error) {
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var summary *dto.SummaryResponse
	if len(job.ResponseJSON) > 0 {
		var decoded dto.SummaryResponse
		if err := json.Unmarshal(job.ResponseJSON, &decoded); err == nil {
			summary = &decoded
		}
	}

	resp := jobResponse(job, summary)
	resp.State = job.State
	resp.SMTPMessageID = job.SMTPMessageID
	resp.ErrorMessage = job.ErrorMessage
	resp.SentAt = job.SentAt
	return resp, nil
}

func jobResponse(job *model.ReportJob, summary *dto.SummaryResponse) *dto.JobResponse {
	return &dto.JobResponse{
		ID:             job.ID.String(),
		ReportType:     job.ReportType,
		State:          job.State,
		StartDate:      job.StartAt.Format(time.DateOnly),
		EndDate:        job.EndAt.Format(time.DateOnly),
		RecipientEmail: job.RecipientEmail,
		Summary:        summary,
		CreatedAt:      job.CreatedAt,
	}
}
