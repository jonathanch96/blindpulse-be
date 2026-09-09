package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/middleware"
	"github.com/jblabs/blindpulse-be/pkg/response"
)

func TestHelpersAlwaysPopulateEnvelopeContainers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		status int
		handle gin.HandlerFunc
	}{
		{"ok", 200, func(c *gin.Context) { response.OK(c, "OK", gin.H{"value": true}) }},
		{"created", 201, func(c *gin.Context) { response.Created(c, "THING_CREATED", gin.H{"id": "1"}) }},
		{"list", 200, func(c *gin.Context) {
			response.List(c, "THINGS_FETCHED", []string{}, response.Pagination{Page: 1, PerPage: 20})
		}},
		{"no-data", 200, func(c *gin.Context) { response.NoData(c, "OK") }},
		{"validation", 400, func(c *gin.Context) {
			response.Error(c, apperror.WithFields("VALIDATION_FAILED", []response.FieldError{{Field: "amount", Rule: "gt", Message: "amount is invalid"}}))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.RequestID())
			engine.GET("/", test.handle)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d", recorder.Code)
			}
			var raw map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"success", "code", "message", "data", "meta", "errors", "trace_id", "timestamp"} {
				if _, ok := raw[key]; !ok {
					t.Errorf("missing %q in %s", key, recorder.Body.String())
				}
			}
			if raw["meta"] == nil || raw["errors"] == nil {
				t.Fatalf("containers must not be null: %s", recorder.Body.String())
			}
		})
	}
}

func TestUnknownErrorDoesNotLeakMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestID())
	engine.GET("/", func(c *gin.Context) { response.Error(c, errors.New("secret SQL detail")) })
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != 500 || contains(recorder.Body.String(), "secret SQL detail") {
		t.Fatalf("unsafe response: %s", recorder.Body.String())
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
