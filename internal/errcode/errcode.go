package errcode

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
)

const (
	CodeUnknown      = "UNKNOWN"
	CodeInvalidURL   = "INVALID_URL"
	CodeNetworkError = "NETWORK_ERROR"
	CodeNetworkTime  = "NETWORK_TIMEOUT"
	CodeCanceled     = "CANCELED"
	CodeFSError      = "FS_ERROR"
	CodeFSPermission = "FS_PERMISSION"
	CodeHTTP4XX      = "HTTP_4XX"
	CodeHTTP408      = "HTTP_408"
	CodeHTTP429      = "HTTP_429"
	CodeHTTP5XX      = "HTTP_5XX"
	CodeRobotsFetch  = "ROBOTS_FETCH_ERROR"
)

// CodedError is implemented by errors that expose a machine-readable code.
type CodedError interface {
	ErrorCode() string
}

// Of returns the best-effort machine-readable code for an error.
func Of(err error) string {
	if err == nil {
		return ""
	}

	var coded CodedError
	if errors.As(err, &coded) {
		if code := strings.TrimSpace(coded.ErrorCode()); code != "" {
			return code
		}
	}

	if errors.Is(err, context.Canceled) {
		return CodeCanceled
	}

	if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
		return CodeNetworkTime
	}

	if os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		return CodeFSPermission
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return CodeFSError
	}

	return CodeUnknown
}

// HTTPStatus maps an HTTP response code to a machine-readable status class code.
func HTTPStatus(statusCode int) string {
	switch {
	case statusCode == 408:
		return CodeHTTP408
	case statusCode == 429:
		return CodeHTTP429
	case statusCode >= 500 && statusCode <= 599:
		return CodeHTTP5XX
	case statusCode >= 400 && statusCode <= 499:
		return CodeHTTP4XX
	default:
		return CodeUnknown
	}
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
