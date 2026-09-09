package response

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/logger"
)

type FieldError = apperror.FieldError

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

type Envelope struct {
	Success   bool         `json:"success"`
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Data      any          `json:"data"`
	Meta      any          `json:"meta"`
	Errors    []FieldError `json:"errors"`
	TraceID   string       `json:"trace_id"`
	Timestamp time.Time    `json:"timestamp" swaggertype:"string" example:"2026-08-05T09:12:33.412Z"`
}

func OK(c *gin.Context, code string, data any) {
	write(c, http.StatusOK, true, code, successMessage(code), data, gin.H{}, []FieldError{})
}

func Created(c *gin.Context, code string, data any) {
	write(c, http.StatusCreated, true, code, successMessage(code), data, gin.H{}, []FieldError{})
}

func List(c *gin.Context, code string, data any, pagination Pagination) {
	write(c, http.StatusOK, true, code, successMessage(code), data, gin.H{"pagination": pagination}, []FieldError{})
}

func ListWithMeta(c *gin.Context, code string, data any, pagination Pagination, extra gin.H) {
	meta := gin.H{"pagination": pagination}
	for key, value := range extra {
		meta[key] = value
	}
	write(c, http.StatusOK, true, code, successMessage(code), data, meta, []FieldError{})
}

func NoData(c *gin.Context, code string) {
	write(c, http.StatusOK, true, code, successMessage(code), nil, gin.H{}, []FieldError{})
}

func Error(c *gin.Context, err error) {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		slog.ErrorContext(c.Request.Context(), "unhandled request error", "err", err)
		appErr = apperror.New("INTERNAL_ERROR")
	} else if appErr.Err != nil {
		slog.ErrorContext(c.Request.Context(), "request failed", "code", appErr.Code, "err", appErr.Err)
	}
	fields := appErr.Fields
	if fields == nil {
		fields = []FieldError{}
	}
	write(c, appErr.HTTP, false, appErr.Code, appErr.Message, nil, gin.H{}, fields)
}

func write(c *gin.Context, status int, success bool, code, message string, data, meta any, fields []FieldError) {
	traceID := logger.TraceID(c.Request.Context())
	if traceID == "" {
		traceID = c.GetHeader("X-Request-ID")
	}
	if meta == nil {
		meta = gin.H{}
	}
	if fields == nil {
		fields = []FieldError{}
	}
	c.JSON(status, Envelope{
		Success: success, Code: code, Message: message, Data: data, Meta: meta,
		Errors: fields, TraceID: traceID, Timestamp: time.Now().UTC().Truncate(time.Millisecond),
	})
}

func successMessage(code string) string {
	if code == "PONG" {
		return "Service is available"
	}
	if code == "OK" {
		return "Request completed successfully"
	}
	return "Request completed successfully"
}
