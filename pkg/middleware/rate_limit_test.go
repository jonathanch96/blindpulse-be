package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimitCountsEachKeyAndReturnsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(2, time.Hour)
	limiter.now = func() time.Time { return now }
	router := gin.New()
	router.GET("/", RateLimit(limiter, func(c *gin.Context) string { return c.GetHeader("X-Test-Key") }), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(key string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Test-Key", key)
		router.ServeHTTP(recorder, req)
		return recorder
	}
	firstA, secondA, firstB := request("a"), request("a"), request("b")
	if firstA.Code != 204 || secondA.Code != 204 || firstB.Code != 204 {
		t.Fatal("independent keys should have independent capacity")
	}
	limited := request("a")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("limited response = %d, Retry-After=%q", limited.Code, limited.Header().Get("Retry-After"))
	}
	now = now.Add(time.Hour)
	if request("a").Code != 204 {
		t.Fatal("capacity should reset with the window")
	}
}
