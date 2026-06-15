package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/fileio-service/config"
	"github.com/vcs-sms/fileio-service/internal/dto"
	"github.com/vcs-sms/fileio-service/internal/excel"
	"github.com/vcs-sms/fileio-service/internal/model"
	"github.com/vcs-sms/fileio-service/internal/repository"
	"github.com/vcs-sms/shared/kafka"
	"gorm.io/gorm"
)

// Sentinel errors
var (
	ErrJobNotFound = fmt.Errorf("import job not found")
)

// ImportService defines the interface for import business logic.
type ImportService interface {
	// InitiateImport validates the uploaded file, saves it, creates a job, and publishes to Kafka.
	InitiateImport(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error)

	// ProcessImportJob is the Kafka consumer handler — parses Excel and imports servers.
	ProcessImportJob(ctx context.Context, jobID string) error

	// GetImportJobStatus returns the current status and details of an import job.
	GetImportJobStatus(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error)
}

// importServiceImpl implements ImportService.
type importServiceImpl struct {
	jobRepo      repository.ImportJobRepo
	serverWriter repository.ServerWriter
	parser       excel.ExcelParser
	producer     kafka.Producer
	cache        cacheInvalidator
	cfg          *config.Config
	log          zerolog.Logger
}

type cacheInvalidator interface {
	DeleteByPattern(ctx context.Context, pattern string)
}

type redisCacheInvalidator struct {
	client *redis.Client
}

func (r *redisCacheInvalidator) DeleteByPattern(ctx context.Context, pattern string) {
	iter := r.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		r.client.Del(ctx, iter.Val())
	}
}

// NewImportService creates a new ImportService instance.
func NewImportService(
	jobRepo repository.ImportJobRepo,
	serverWriter repository.ServerWriter,
	parser excel.ExcelParser,
	producer kafka.Producer,
	rdb *redis.Client,
	cfg *config.Config,
	log zerolog.Logger,
) ImportService {
	var cache cacheInvalidator
	if rdb != nil {
		cache = &redisCacheInvalidator{client: rdb}
	}
	return &importServiceImpl{
		jobRepo:      jobRepo,
		serverWriter: serverWriter,
		parser:       parser,
		producer:     producer,
		cache:        cache,
		cfg:          cfg,
		log:          log,
	}
}

// InitiateImport handles the synchronous part of the import flow.
func (s *importServiceImpl) InitiateImport(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (*dto.ImportJobResponse, error) {
	// 1. Validate file extension
	ext := filepath.Ext(header.Filename)
	if !strings.EqualFold(ext, ".xlsx") {
		return nil, fmt.Errorf("invalid file format: only .xlsx files are accepted")
	}

	// 2. Validate file size
	maxSize := int64(s.cfg.App.MaxFileSizeMB) * 1024 * 1024
	if header.Size > maxSize {
		return nil, fmt.Errorf("file too large: maximum %d MB", s.cfg.App.MaxFileSizeMB)
	}

	// 3. Generate job ID and save file
	jobID := uuid.New()
	uploadDir := s.cfg.App.UploadDir
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	filePath := filepath.Join(uploadDir, jobID.String()+".xlsx")
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to save uploaded file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(filePath) // clean up
		return nil, fmt.Errorf("failed to write uploaded file: %w", err)
	}

	// 3.5 Quick-validate the workbook and expected headers without parsing all rows.
	if err := s.parser.ValidateHeaders(filePath); err != nil {
		os.Remove(filePath) // clean up — invalid file
		return nil, fmt.Errorf("invalid Excel file: %w", err)
	}

	// 4. Create import_job record
	var createdBy *uuid.UUID
	if uid, err := uuid.Parse(userID); err == nil {
		createdBy = &uid
	}

	now := time.Now().UTC()
	job := &model.ImportJob{
		ID:        jobID,
		Status:    "pending",
		FileName:  header.Filename,
		FilePath:  filePath,
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.jobRepo.Create(ctx, job); err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to create import job: %w", err)
	}

	// 5. Publish Kafka event for async processing
	event := &kafka.Event{
		EventID:   uuid.New().String(),
		EventType: "import.job.created",
		Timestamp: now.Format(time.RFC3339),
		Source:    "fileio-service",
		Data: map[string]interface{}{
			"job_id":    jobID.String(),
			"file_path": filePath,
			"user_id":   userID,
		},
	}

	if err := s.producer.Publish(ctx, "import.job.created", jobID.String(), event); err != nil {
		_ = s.jobRepo.UpdateFailed(ctx, jobID.String(), fmt.Sprintf("Failed to queue import job: %v", err))
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to queue import job: %w", err)
	}

	return &dto.ImportJobResponse{
		JobID:    jobID.String(),
		Status:   "pending",
		FileName: header.Filename,
		Message:  "File received and queued for processing",
	}, nil
}

// ProcessImportJob is the Kafka consumer handler for async import processing.
func (s *importServiceImpl) ProcessImportJob(ctx context.Context, jobID string) error {
	s.log.Info().Str("job_id", jobID).Msg("Processing import job")

	// 1. Update status to processing
	if err := s.jobRepo.UpdateStatus(ctx, jobID, "processing"); err != nil {
		return fmt.Errorf("failed to update job status to processing: %w", err)
	}

	// 2. Find the job to get file path
	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Job not found: %v", err))
		return fmt.Errorf("job not found: %w", err)
	}

	// 3. Parse Excel file
	rows, err := s.parser.Parse(job.FilePath)
	if err != nil {
		_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Failed to parse file: %v", err))
		return fmt.Errorf("failed to parse excel: %w", err)
	}

	// 4. Process each row
	successCount := 0
	failedCount := 0

	for _, row := range rows {
		detail := model.ImportJobDetail{
			ImportJobID: job.ID,
			RowNumber:   row.RowNumber,
			ServerID:    row.ServerID,
			ServerName:  row.ServerName,
		}

		// Row validation failed
		if !row.IsValid {
			detail.Status = "failed"
			detail.ErrorReason = row.ErrorMsg
			if err := s.jobRepo.SaveDetail(ctx, &detail); err != nil {
				_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Failed to save row detail: %v", err))
				return fmt.Errorf("failed to save row detail: %w", err)
			}
			failedCount++
			continue
		}

		// Check for duplicates in server_schema
		existing, err := s.serverWriter.FindByServerIDOrName(ctx, row.ServerID, row.ServerName)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			detail.Status = "failed"
			detail.ErrorReason = fmt.Sprintf("duplicate check error: %v", err)
			if err := s.jobRepo.SaveDetail(ctx, &detail); err != nil {
				_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Failed to save row detail: %v", err))
				return fmt.Errorf("failed to save row detail: %w", err)
			}
			failedCount++
			continue
		}
		if err == nil && existing != nil {
			// Duplicate found
			detail.Status = "failed"
			if existing.ServerID == row.ServerID {
				detail.ErrorReason = fmt.Sprintf("duplicate server_id: %s", row.ServerID)
			} else {
				detail.ErrorReason = fmt.Sprintf("duplicate server_name: %s", row.ServerName)
			}
			if err := s.jobRepo.SaveDetail(ctx, &detail); err != nil {
				_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Failed to save row detail: %v", err))
				return fmt.Errorf("failed to save row detail: %w", err)
			}
			failedCount++
			continue
		}

		// Insert new server via serverWriter (uses server_schema connection)
		now := time.Now().UTC()
		server := &model.Server{
			ServerID:    row.ServerID,
			ServerName:  row.ServerName,
			Status:      "off",
			IPv4:        row.IPv4,
			OS:          row.OS,
			CPUCores:    row.CPUCores,
			RAMGB:       row.RAMGB,
			DiskGB:      row.DiskGB,
			Location:    row.Location,
			Description: row.Description,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := s.serverWriter.Create(ctx, server); err != nil {
			detail.Status = "failed"
			detail.ErrorReason = fmt.Sprintf("database error: %v", err)
			if saveErr := s.jobRepo.SaveDetail(ctx, &detail); saveErr != nil {
				_ = s.jobRepo.UpdateFailed(ctx, jobID, fmt.Sprintf("Failed to save row detail: %v", saveErr))
				return fmt.Errorf("failed to save row detail: %w", saveErr)
			}
			failedCount++
			continue
		}

		// Save success detail
		detail.Status = "success"
		if err := s.jobRepo.SaveDetail(ctx, &detail); err != nil {
			// Detail save failed but server was created — log and continue
			s.log.Warn().Err(err).Str("server_id", row.ServerID).Msg("Server created but failed to save detail record")
		}

		s.publishServerCreated(ctx, row.ServerID, server)
		successCount++
	}

	// 5. Mark job as completed
	if err := s.jobRepo.UpdateCompleted(ctx, jobID, len(rows), successCount, failedCount); err != nil {
		return fmt.Errorf("failed to update job as completed: %w", err)
	}

	// 6. Invalidate Redis cache for server lists
	s.invalidateCache(ctx)

	s.log.Info().
		Str("job_id", jobID).
		Int("total", len(rows)).
		Int("success", successCount).
		Int("failed", failedCount).
		Msg("Import job completed")

	return nil
}

func (s *importServiceImpl) publishServerCreated(ctx context.Context, serverID string, server *model.Server) {
	event := &kafka.Event{
		EventID:   uuid.New().String(),
		EventType: "server.created",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "fileio-service",
		Data:      server,
	}
	if err := s.producer.Publish(ctx, "server.created", serverID, event); err != nil {
		s.log.Warn().Err(err).Str("server_id", serverID).Msg("Failed to publish server.created event")
	}
}

// GetImportJobStatus retrieves the current status and details of an import job.
func (s *importServiceImpl) GetImportJobStatus(ctx context.Context, jobID string) (*dto.ImportJobStatusResponse, error) {
	job, err := s.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("import job not found: %w", err)
	}

	details, err := s.jobRepo.GetDetailsByJobID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job details: %w", err)
	}

	// Build response
	resp := &dto.ImportJobStatusResponse{
		JobID:        job.ID.String(),
		Status:       job.Status,
		FileName:     job.FileName,
		TotalRows:    job.TotalRows,
		SuccessCount: job.SuccessCount,
		FailedCount:  job.FailedCount,
		ErrorMessage: job.ErrorMessage,
		CreatedAt:    job.CreatedAt.Format(time.RFC3339),
	}

	if job.StartedAt != nil {
		s := job.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &s
	}
	if job.CompletedAt != nil {
		c := job.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &c
	}

	// Separate success and failed rows
	for _, d := range details {
		row := dto.ImportRowResult{
			RowNumber:  d.RowNumber,
			ServerID:   d.ServerID,
			ServerName: d.ServerName,
			Status:     d.Status,
			Reason:     d.ErrorReason,
		}
		if d.Status == "success" {
			resp.SuccessList = append(resp.SuccessList, row)
		} else {
			resp.FailedList = append(resp.FailedList, row)
		}
	}

	return resp, nil
}

// invalidateCache removes Redis cache entries related to server lists.
func (s *importServiceImpl) invalidateCache(ctx context.Context) {
	if s.cache == nil {
		return
	}

	s.cache.DeleteByPattern(ctx, "server:detail:*")
	s.cache.DeleteByPattern(ctx, "servers:list:*")
}
