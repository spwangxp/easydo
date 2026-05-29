package services

type ErrorCode string

const (
	ErrorCodeUnauthorized       ErrorCode = "unauthorized"
	ErrorCodeForbidden          ErrorCode = "forbidden"
	ErrorCodeNotFound           ErrorCode = "not_found"
	ErrorCodeInvalidArgument    ErrorCode = "invalid_argument"
	ErrorCodeConflict           ErrorCode = "conflict"
	ErrorCodeRateLimited        ErrorCode = "rate_limited"
	ErrorCodeConcurrencyLimited ErrorCode = "concurrency_limited"
	ErrorCodeTimeout            ErrorCode = "timeout"
	ErrorCodeInternalError      ErrorCode = "internal_error"
)

type ServiceError struct {
	Code    ErrorCode
	Message string
}

func (e ServiceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func NewServiceError(code ErrorCode, message string) ServiceError {
	return ServiceError{Code: code, Message: message}
}
