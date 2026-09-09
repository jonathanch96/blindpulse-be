package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appLogger "github.com/jblabs/blindpulse-be/pkg/logger"
	"github.com/jblabs/blindpulse-be/pkg/response"
)

func TestRecoveryReturnsEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	engine := gin.New()
	engine.Use(RequestID(), Recovery(appLogger.NewForTest(&logs)))
	engine.GET("/panic", func(_ *gin.Context) { panic("boom") })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	request.Header.Set("X-Request-ID", "01K20TESTTRACE0000000000000")
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "INTERNAL_ERROR" || envelope.TraceID != request.Header.Get("X-Request-ID") {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if bytes.Count(logs.Bytes(), []byte("panic recovered")) != 1 {
		t.Fatalf("panic log count != 1: %s", logs.String())
	}
}
