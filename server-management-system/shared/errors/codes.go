package errors

// Error codes theo thiết kế mới
const (
	// Common
	CodeValidationFailed = "COMMON_VALIDATION_FAILED"
	CodeUnauthorized     = "COMMON_UNAUTHORIZED"
	CodeForbiddenScope   = "COMMON_FORBIDDEN_SCOPE"
	CodeNotFound         = "COMMON_NOT_FOUND"
	CodeRateLimited      = "COMMON_RATE_LIMITED"
	CodeInternalError    = "COMMON_INTERNAL_ERROR"
	CodeConflict         = "COMMON_CONFLICT"

	// Auth
	CodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	CodeAccountLocked      = "AUTH_ACCOUNT_LOCKED"

	// Server
	CodeDuplicateServerID    = "SERVER_DUPLICATE_ID"
	CodeDuplicateServerName  = "SERVER_DUPLICATE_NAME"
	CodeServerValidation     = "SERVER_VALIDATION_FAILED"
	CodeServerIPNotAllowed   = "SERVER_IP_NOT_ALLOWED"
	CodeServerImportRejected = "SERVER_IMPORT_FILE_REJECTED"
	CodeIdempotencyConflict  = "SERVER_IDEMPOTENCY_CONFLICT"

	// Report
	CodeReportInvalidRange     = "REPORT_INVALID_RANGE"
	CodeReportRecipientBlocked = "REPORT_RECIPIENT_NOT_ALLOWED"
	CodeReportIdempotency      = "REPORT_IDEMPOTENCY_CONFLICT"
	CodeReportDataUnavailable  = "REPORT_DATA_UNAVAILABLE"
)

// HTTP status mapping
var ErrorHTTPStatus = map[string]int{
	CodeValidationFailed:       422,
	CodeUnauthorized:           401,
	CodeForbiddenScope:         403,
	CodeNotFound:               404,
	CodeRateLimited:            429,
	CodeInternalError:          500,
	CodeConflict:               409,
	CodeInvalidCredentials:     401,
	CodeAccountLocked:          423,
	CodeDuplicateServerID:      409,
	CodeDuplicateServerName:    409,
	CodeServerValidation:       422,
	CodeServerIPNotAllowed:     422,
	CodeServerImportRejected:   422,
	CodeIdempotencyConflict:    409,
	CodeReportInvalidRange:     422,
	CodeReportRecipientBlocked: 422,
	CodeReportIdempotency:      409,
	CodeReportDataUnavailable:  503,
}
