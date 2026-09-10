package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
)

func init() { gin.SetMode(gin.TestMode) }

// This test exists because of review finding BE-01-1, and it tests the thing that finding was
// about. `pkg/middleware` already had a rate limiter with its own passing tests — what was missing
// was any assertion that a request to a real route goes through it. "We wrote a limiter" and
// "requests are limited" turned out to be different claims, and only the second one matters.
//
// So this builds the engine the way the application builds it, through NewService and
// RegisterRoutes, and asks whether an unauthenticated caller can hammer /auth/login.
func newTestEngine(t *testing.T, addressAttempts, emailAttempts int) *gin.Engine {
	t.Helper()
	cfg := &config.Config{}
	cfg.App.Env = "test"
	cfg.JWT.AccessSecret = "test-access-secret-not-used-here"
	cfg.JWT.RefreshSecret = "test-refresh-secret-not-used-here"
	cfg.JWT.AccessTTL = time.Minute
	cfg.JWT.RefreshTTL = time.Hour
	cfg.Auth.AddressAttempts = addressAttempts
	cfg.Auth.EmailAttempts = emailAttempts
	cfg.Auth.Window = time.Hour

	// A nil database is enough: the assertions are about which requests reach a handler at all.
	// One that gets past the throttle panics on the nil handle, and the recovery below turns that
	// into a 500 — which is the signal that it was *not* throttled.
	service := NewService(Dependencies{DB: nil, Cfg: cfg})
	engine := gin.New()
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { c.AbortWithStatus(http.StatusInternalServerError) }))
	service.RegisterRoutes(engine.Group("/api/v1"))
	return engine
}

func post(t *testing.T, engine *gin.Engine, path, body, address string) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = address + ":44321"
	engine.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestLoginIsThrottledOnTheRealRouteTable(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t, 2, 100)
	const body = `{"email":"trader@example.com","password":"hunter2"}`

	// The first two reach a handler and fail on the nil database — that is fine, and it is what
	// proves they were *not* rejected by the limiter.
	for attempt := 1; attempt <= 2; attempt++ {
		if code := post(t, engine, "/api/v1/auth/login", body, "203.0.113.7"); code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was throttled too early", attempt)
		}
	}
	if code := post(t, engine, "/api/v1/auth/login", body, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Errorf("third login from one address = %d, want 429 — the limiter is not mounted", code)
	}
}

func TestRegisterIsThrottledOnTheRealRouteTable(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t, 1, 100)
	const body = `{"email":"new@example.com","password":"hunter2","name":"New"}`

	if code := post(t, engine, "/api/v1/auth/register", body, "198.51.100.4"); code == http.StatusTooManyRequests {
		t.Fatal("the first registration was throttled")
	}
	if code := post(t, engine, "/api/v1/auth/register", body, "198.51.100.4"); code != http.StatusTooManyRequests {
		t.Errorf("second registration from one address = %d, want 429", code)
	}
}

// One account sprayed from many addresses is the attack the address limiter cannot see.
func TestOneAccountCannotBeSprayedFromManyAddresses(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t, 100, 2)
	const body = `{"email":"victim@example.com","password":"guess"}`

	for _, address := range []string{"203.0.113.1", "203.0.113.2"} {
		if code := post(t, engine, "/api/v1/auth/login", body, address); code == http.StatusTooManyRequests {
			t.Fatalf("attempt from %s was throttled too early", address)
		}
	}
	if code := post(t, engine, "/api/v1/auth/login", body, "203.0.113.3"); code != http.StatusTooManyRequests {
		t.Errorf("third address attacking one account = %d, want 429", code)
	}
}

// The throttle belongs to the auth group only. Applying it to the whole API would count a trader's
// ordinary requests against the same budget as an attacker's login attempts.
func TestTheThrottleDoesNotCoverTheRestOfTheApi(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t, 1, 1)
	// Spend the auth budget.
	post(t, engine, "/api/v1/auth/login", `{"email":"a@example.com","password":"x"}`, "203.0.113.9")
	post(t, engine, "/api/v1/auth/login", `{"email":"a@example.com","password":"x"}`, "203.0.113.9")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	request.RemoteAddr = "203.0.113.9:44321"
	engine.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusTooManyRequests {
		t.Error("an unrelated endpoint was throttled by the auth budget")
	}
}
