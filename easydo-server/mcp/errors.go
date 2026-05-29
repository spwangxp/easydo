package mcp

import (
	"errors"

	"easydo-server/internal/services"
)

const (
	jsonRPCCodeMethodNotFound = -32601
	jsonRPCCodeApplication    = -32000
)

func MapError(err error) *JSONRPCError {
	if err == nil {
		return nil
	}

	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		return mapServiceError(svcErr)
	}

	var svcErrPtr *services.ServiceError
	if errors.As(err, &svcErrPtr) && svcErrPtr != nil {
		return mapServiceError(*svcErrPtr)
	}

	return mapServiceError(services.ServiceError{
		Code:    services.ErrorCodeInternalError,
		Message: "internal error",
	})
}

func mapServiceError(svcErr services.ServiceError) *JSONRPCError {
	message := svcErr.Message
	if message == "" {
		message = string(svcErr.Code)
	}
	return &JSONRPCError{
		Code:    jsonRPCCodeApplication,
		Message: message,
		Data: map[string]any{
			"code": string(svcErr.Code),
		},
	}
}
