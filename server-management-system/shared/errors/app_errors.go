package errors

import "net/http"

// AppError represents an application-level error with code and message
type AppError struct {
	HTTPCode int    `json:"-"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func (e *AppError) Error() string {
	return e.Message
}

// Standard application errors
var (
	ErrBadRequest = &AppError{
		HTTPCode: http.StatusBadRequest,
		Code:     "BAD_REQUEST",
		Message:  "Request body không hợp lệ",
	}

	ErrUnauthorized = &AppError{
		HTTPCode: http.StatusUnauthorized,
		Code:     "UNAUTHORIZED",
		Message:  "Token không hợp lệ hoặc đã hết hạn",
	}

	ErrTokenRevoked = &AppError{
		HTTPCode: http.StatusUnauthorized,
		Code:     "TOKEN_REVOKED",
		Message:  "Token đã bị thu hồi",
	}

	ErrForbidden = &AppError{
		HTTPCode: http.StatusForbidden,
		Code:     "FORBIDDEN",
		Message:  "Bạn không có quyền truy cập tài nguyên này",
	}

	ErrNotFound = &AppError{
		HTTPCode: http.StatusNotFound,
		Code:     "NOT_FOUND",
		Message:  "Tài nguyên không tồn tại",
	}

	ErrConflict = &AppError{
		HTTPCode: http.StatusConflict,
		Code:     "CONFLICT",
		Message:  "Dữ liệu đã tồn tại",
	}

	ErrDuplicateServerID = &AppError{
		HTTPCode: http.StatusConflict,
		Code:     "DUPLICATE_SERVER_ID",
		Message:  "server_id đã tồn tại",
	}

	ErrDuplicateServerName = &AppError{
		HTTPCode: http.StatusConflict,
		Code:     "DUPLICATE_SERVER_NAME",
		Message:  "server_name đã tồn tại",
	}

	ErrDuplicateEmail = &AppError{
		HTTPCode: http.StatusConflict,
		Code:     "DUPLICATE_EMAIL",
		Message:  "Email đã được sử dụng",
	}

	ErrInvalidCredentials = &AppError{
		HTTPCode: http.StatusUnauthorized,
		Code:     "INVALID_CREDENTIALS",
		Message:  "Email hoặc password không đúng",
	}

	ErrInactiveUser = &AppError{
		HTTPCode: http.StatusForbidden,
		Code:     "INACTIVE_USER",
		Message:  "Tài khoản đã bị vô hiệu hóa",
	}

	ErrRateLimitExceeded = &AppError{
		HTTPCode: http.StatusTooManyRequests,
		Code:     "RATE_LIMIT_EXCEEDED",
		Message:  "Bạn đã gửi quá nhiều request, vui lòng thử lại sau",
	}

	ErrInternalServer = &AppError{
		HTTPCode: http.StatusInternalServerError,
		Code:     "INTERNAL_ERROR",
		Message:  "Lỗi hệ thống, vui lòng thử lại sau",
	}

	ErrDBError = &AppError{
		HTTPCode: http.StatusInternalServerError,
		Code:     "DB_ERROR",
		Message:  "Lỗi database",
	}

	ErrESError = &AppError{
		HTTPCode: http.StatusInternalServerError,
		Code:     "ES_ERROR",
		Message:  "Lỗi Elasticsearch",
	}

	ErrEmailError = &AppError{
		HTTPCode: http.StatusInternalServerError,
		Code:     "EMAIL_ERROR",
		Message:  "Lỗi gửi email",
	}
)

// NewAppError creates a custom AppError
func NewAppError(httpCode int, code, message string) *AppError {
	return &AppError{
		HTTPCode: httpCode,
		Code:     code,
		Message:  message,
	}
}
