package slack

import (
	"context"
	"net"
	"strings"
	"syscall"
)

// ErrorType represents a classification of errors for user-friendly messages
type ErrorType int

const (
	ErrorTypeUnknown ErrorType = iota
	ErrorTypeTimeout
	ErrorTypeConnectionRefused
	ErrorTypeServerError
	ErrorTypeOverloaded
	ErrorTypePayloadTooLarge
)

// FriendlyErrorMessages maps error types to user-friendly messages in Spanish
// These messages are designed to be helpful without exposing technical details
var FriendlyErrorMessages = map[ErrorType]string{
	ErrorTypeTimeout:           ":hourglass: La operacion tardo demasiado. Por favor, intenta de nuevo en unos momentos.",
	ErrorTypeConnectionRefused: ":warning: No puedo conectarme al servicio en este momento. El equipo ha sido notificado.",
	ErrorTypeServerError:       ":gear: Tengo un problema interno procesando tu solicitud. Por favor, reintenta en unos minutos.",
	ErrorTypeOverloaded:        ":traffic_light: El sistema esta procesando muchas solicitudes. Por favor, intenta de nuevo en un momento.",
	ErrorTypePayloadTooLarge:   ":package: El mensaje es demasiado grande para procesar. Intenta con un mensaje mas corto o menos imagenes.",
	ErrorTypeUnknown:           ":thinking_face: Algo no salio como esperaba. Por favor, intenta de nuevo.",
}

// ClassifyError determines the error type based on the error and HTTP status code
func ClassifyError(err error, statusCode int) ErrorType {
	// Check specific status codes first (before generic 5xx range)
	switch statusCode {
	case 408, 504: // Request Timeout, Gateway Timeout
		return ErrorTypeTimeout
	case 413: // Payload Too Large
		return ErrorTypePayloadTooLarge
	case 503: // Service Unavailable
		return ErrorTypeOverloaded
	}

	// Generic 5xx server errors
	if statusCode >= 500 && statusCode < 600 {
		return ErrorTypeServerError
	}

	// Check error types
	if err == nil {
		return ErrorTypeUnknown
	}

	errStr := err.Error()

	// Context errors (timeout/canceled)
	if err == context.DeadlineExceeded || strings.Contains(errStr, "context deadline exceeded") {
		return ErrorTypeTimeout
	}
	if err == context.Canceled || strings.Contains(errStr, "context canceled") {
		return ErrorTypeTimeout
	}

	// Network errors
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return ErrorTypeTimeout
		}
	}

	// Connection refused
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*syscall.Errno); ok {
			if *sysErr == syscall.ECONNREFUSED {
				return ErrorTypeConnectionRefused
			}
		}
	}
	if strings.Contains(errStr, "connection refused") {
		return ErrorTypeConnectionRefused
	}

	// EOF or connection closed
	if strings.Contains(errStr, "EOF") || strings.Contains(errStr, "connection reset") {
		return ErrorTypeOverloaded
	}

	// Timeout patterns
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "timed out") {
		return ErrorTypeTimeout
	}

	return ErrorTypeUnknown
}

// FormatUserError returns a user-friendly error message based on the error and status code
// Technical details are logged separately and not exposed to the user
func FormatUserError(err error, statusCode int) string {
	errorType := ClassifyError(err, statusCode)
	if msg, ok := FriendlyErrorMessages[errorType]; ok {
		return msg
	}
	return FriendlyErrorMessages[ErrorTypeUnknown]
}
