package service

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/dto"
	"github.com/vcs-sms/server-service/internal/infrastructure/cache"
	"github.com/vcs-sms/server-service/internal/infrastructure/excel"
	"github.com/vcs-sms/server-service/internal/infrastructure/projection"
	"github.com/vcs-sms/server-service/internal/model"
	"github.com/vcs-sms/server-service/internal/repository"
	"github.com/vcs-sms/server-service/internal/validator"
	"gorm.io/gorm"
)

// ErrImportFileRejected marks a file-level problem that rejects every row.
var ErrImportFileRejected = errors.New("import file rejected")

const importBatchSize = 500

// ImportService imports servers from an Excel file within one request.
type ImportService interface {
	Import(ctx context.Context, r io.Reader) (*dto.ImportResponse, error)
}

type importServiceImpl struct {
	repo    repository.ServerRepository
	parser  excel.Parser
	cidr    *validator.CIDRValidator
	targets projection.TargetProjection
	cache   serverCache
	log     zerolog.Logger
}

// NewImportService creates an ImportService.
func NewImportService(
	repo repository.ServerRepository,
	parser excel.Parser,
	cidr *validator.CIDRValidator,
	targets projection.TargetProjection,
	rdb *redis.Client,
	log zerolog.Logger,
) ImportService {
	var c serverCache
	if rdb != nil {
		c = &redisServerCache{client: rdb}
	}
	return &importServiceImpl{
		repo:    repo,
		parser:  parser,
		cidr:    cidr,
		targets: targets,
		cache:   c,
		log:     log,
	}
}

// importState accumulates the three outcome groups.
type importState struct {
	succeeded []string
	failed    []dto.ImportFailedItem
	skipped   []string
}

func (s *importState) fail(row int, serverID, reason string) {
	s.failed = append(s.failed, dto.ImportFailedItem{Row: row, ServerID: serverID, Reason: reason})
}

// Import parses, validates and inserts, reporting each row's outcome. A row
// problem never fails the request; only a file problem does.
func (s *importServiceImpl) Import(ctx context.Context, r io.Reader) (*dto.ImportResponse, error) {
	rows, err := s.parser.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrImportFileRejected, err)
	}

	state := &importState{}
	candidates := s.validate(rows, state)
	candidates = s.dropExistingNames(ctx, candidates, state)
	s.insert(ctx, candidates, state)

	if len(state.succeeded) > 0 {
		s.bumpListVersion(ctx)
	}

	return &dto.ImportResponse{
		TotalRows: len(rows),
		Succeeded: dto.ImportSucceeded{Count: len(state.succeeded), Items: emptyIfNil(state.succeeded)},
		Failed:    dto.ImportFailed{Count: len(state.failed), Items: failedOrEmpty(state.failed)},
		SkippedDuplicate: dto.ImportSkipped{
			Count: len(state.skipped),
			Items: emptyIfNil(state.skipped),
		},
	}, nil
}

// validate drops rows the parser rejected, rows outside the CIDR allowlist, and
// rows duplicated within the file itself. The first occurrence of a duplicate wins.
func (s *importServiceImpl) validate(rows []excel.ParsedRow, state *importState) []excel.ParsedRow {
	seenIDs := make(map[string]bool, len(rows))
	seenNames := make(map[string]bool, len(rows))
	out := make([]excel.ParsedRow, 0, len(rows))

	for _, row := range rows {
		if !row.IsValid {
			state.fail(row.RowNumber, row.ServerID, row.ErrorCode)
			continue
		}
		if err := s.cidr.Validate(row.IPv4); err != nil {
			state.fail(row.RowNumber, row.ServerID, "SERVER_IP_NOT_ALLOWED")
			continue
		}
		if seenIDs[row.ServerID] || seenNames[row.ServerName] {
			state.skipped = append(state.skipped, row.ServerID)
			continue
		}
		seenIDs[row.ServerID] = true
		seenNames[row.ServerName] = true
		out = append(out, row)
	}
	return out
}

// dropExistingNames removes rows whose name is already taken by an active
// server. ON CONFLICT handles only server_id, so names are filtered up front.
func (s *importServiceImpl) dropExistingNames(ctx context.Context, rows []excel.ParsedRow, state *importState) []excel.ParsedRow {
	if len(rows) == 0 {
		return rows
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.ServerName)
	}

	existing, err := s.repo.FindExistingNames(ctx, names)
	if err != nil {
		// The insert still catches clashes via the per-row fallback.
		s.log.Warn().Err(err).Msg("Name pre-check failed; relying on insert fallback")
		return rows
	}

	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n] = true
	}

	out := make([]excel.ParsedRow, 0, len(rows))
	for _, row := range rows {
		if taken[row.ServerName] {
			state.skipped = append(state.skipped, row.ServerID)
			continue
		}
		out = append(out, row)
	}
	return out
}

// insert writes rows in batches, each batch a single statement.
func (s *importServiceImpl) insert(ctx context.Context, rows []excel.ParsedRow, state *importState) {
	for start := 0; start < len(rows); start += importBatchSize {
		end := min(start+importBatchSize, len(rows))
		s.insertBatch(ctx, rows[start:end], state)
	}
}

func (s *importServiceImpl) insertBatch(ctx context.Context, rows []excel.ParsedRow, state *importState) {
	servers := make([]model.Server, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, rowToServer(row))
	}

	inserted, err := s.repo.InsertBatch(ctx, servers)
	if err != nil {
		// A name clash raced past the pre-check and aborted the whole batch, so
		// retry row by row rather than lose the rows that are still good.
		if repository.IsUniqueViolation(err, repository.ActiveNameConstraint) {
			s.log.Info().Int("rows", len(rows)).Msg("Batch hit a name clash; retrying per row")
			s.insertPerRow(ctx, rows, state)
			return
		}
		for _, row := range rows {
			state.fail(row.RowNumber, row.ServerID, "DATABASE_ERROR")
		}
		s.log.Error().Err(err).Int("rows", len(rows)).Msg("Import batch failed")
		return
	}

	insertedIDs := make(map[string]bool, len(inserted))
	for _, id := range inserted {
		insertedIDs[id] = true
	}
	for _, row := range rows {
		if insertedIDs[row.ServerID] {
			state.succeeded = append(state.succeeded, row.ServerID)
			s.syncImportedTarget(ctx, row)
			continue
		}
		// Absent from RETURNING means ON CONFLICT (server_id) skipped it.
		state.skipped = append(state.skipped, row.ServerID)
	}
}

// insertPerRow retries a clashing batch one row at a time so one bad row cannot
// cost the rest of the batch.
func (s *importServiceImpl) insertPerRow(ctx context.Context, rows []excel.ParsedRow, state *importState) {
	for _, row := range rows {
		server := rowToServer(row)
		err := s.repo.InsertOne(ctx, &server)
		switch {
		case err == nil:
			state.succeeded = append(state.succeeded, row.ServerID)
			s.syncImportedTarget(ctx, row)
		case errors.Is(err, gorm.ErrRecordNotFound),
			repository.IsUniqueViolation(err, repository.ActiveNameConstraint):
			state.skipped = append(state.skipped, row.ServerID)
		default:
			state.fail(row.RowNumber, row.ServerID, "DATABASE_ERROR")
			s.log.Error().Err(err).Str("server_id", row.ServerID).Msg("Import row failed")
		}
	}
}

func (s *importServiceImpl) syncImportedTarget(ctx context.Context, row excel.ParsedRow) {
	if s.targets == nil {
		return
	}
	target := projection.Target{
		ServerID:   row.ServerID,
		ServerName: row.ServerName,
		IPv4:       row.IPv4,
		TCPPort:    row.TCPPort,
	}
	if err := s.targets.Sync(ctx, target); err != nil {
		s.log.Error().Err(err).Str("server_id", row.ServerID).
			Msg("Failed to sync imported target projection")
	}
}

func (s *importServiceImpl) bumpListVersion(ctx context.Context) {
	if s.cache != nil {
		_ = s.cache.Incr(ctx, cache.ListVersionKey)
	}
}

func rowToServer(row excel.ParsedRow) model.Server {
	return model.Server{
		ServerID:    row.ServerID,
		ServerName:  row.ServerName,
		Status:      "UNKNOWN",
		IPv4:        row.IPv4,
		TCPPort:     row.TCPPort,
		OS:          row.OS,
		CPUCores:    optionalInt(row.CPUCores),
		RAMGB:       optionalInt(row.RAMGB),
		DiskGB:      optionalInt(row.DiskGB),
		Location:    row.Location,
		Description: row.Description,
	}
}

func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func failedOrEmpty(in []dto.ImportFailedItem) []dto.ImportFailedItem {
	if in == nil {
		return []dto.ImportFailedItem{}
	}
	return in
}
