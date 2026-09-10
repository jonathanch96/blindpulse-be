package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/auth/login", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = "203.0.113.7:44321"
	return c, recorder
}

func TestEmailKeyCountsTheAccountBeingAttempted(t *testing.T) {
	t.Parallel()
	c, _ := newContext(http.MethodPost, `{"email":"Trader@Example.com","password":"hunter2"}`)
	if got := EmailKey(c); got != "email:trader@example.com" {
		t.Errorf("EmailKey = %q, want the normalized address", got)
	}
}

// The middleware has to put the body back. One that consumed it would turn every login into a
// validation error, which is a worse outage than the one the limiter prevents.
func TestEmailKeyLeavesTheBodyReadableForTheHandler(t *testing.T) {
	t.Parallel()
	body := `{"email":"trader@example.com","password":"hunter2"}`
	c, _ := newContext(http.MethodPost, body)
	_ = EmailKey(c)

	replayed, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("body unreadable after keying: %v", err)
	}
	if string(replayed) != body {
		t.Errorf("body after keying = %q, want it unchanged", replayed)
	}
	var payload map[string]string
	if err := json.Unmarshal(replayed, &payload); err != nil || payload["password"] != "hunter2" {
		t.Errorf("handler could not bind the restored body: %v", err)
	}
}

// Falling back to the address rather than skipping the limiter: otherwise "send garbage" is the
// way around it.
func TestEmailKeyFallsBackToTheAddressRatherThanSkipping(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "not json", `{"email":""}`, `{"email":"   "}`} {
		c, _ := newContext(http.MethodPost, body)
		if got := EmailKey(c); !strings.HasPrefix(got, "ip:") {
			t.Errorf("EmailKey(%q) = %q, want an address fallback", body, got)
		}
	}
}

// An attacker choosing the body size chooses how much memory each rejected request costs.
func TestEmailKeyRefusesToReadAnUnboundedBody(t *testing.T) {
	t.Parallel()
	huge := `{"email":"trader@example.com","padding":"` + strings.Repeat("a", 64<<10) + `"}`
	c, _ := newContext(http.MethodPost, huge)
	// Truncated at the cap, so it no longer parses as JSON and falls back — the point is that it
	// returns rather than reading 64KB.
	if got := EmailKey(c); !strings.HasPrefix(got, "ip:") {
		t.Errorf("EmailKey on an oversized body = %q, want an address fallback", got)
	}
	read, _ := io.ReadAll(c.Request.Body)
	if len(read) > maxCredentialBody {
		t.Errorf("read %d bytes of the body, want at most %d", len(read), maxCredentialBody)
	}
}

// Two independent budgets, not one composite key. A composite would give every (address, email)
// pair its own allowance, which stops neither credential stuffing nor password spraying.
func TestBothLimitersMustPass(t *testing.T) {
	t.Parallel()
	byAddress := NewRateLimiter(100, time.Hour)
	byEmail := NewRateLimiter(2, time.Hour)

	router := gin.New()
	group := router.Group("")
	group.Use(ThrottleAuth(byAddress, byEmail)...)
	group.POST("/auth/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	attempt := func(email string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/auth/login",
			bytes.NewReader([]byte(`{"email":"`+email+`","password":"x"}`)))
		request.RemoteAddr = "203.0.113.7:44321"
		router.ServeHTTP(recorder, request)
		return recorder.Code
	}

	// One account sprayed: the email budget binds even though the address budget is wide open.
	if code := attempt("victim@example.com"); code != http.StatusNoContent {
		t.Fatalf("first attempt = %d", code)
	}
	if code := attempt("victim@example.com"); code != http.StatusNoContent {
		t.Fatalf("second attempt = %d", code)
	}
	if code := attempt("victim@example.com"); code != http.StatusTooManyRequests {
		t.Errorf("third attempt on one account = %d, want 429", code)
	}
	// A different account from the same address is unaffected: the budgets are per-key.
	if code := attempt("someone-else@example.com"); code != http.StatusNoContent {
		t.Errorf("a different account = %d, want it unaffected", code)
	}
}

func TestAddressLimiterCatchesManyAccountsFromOneAddress(t *testing.T) {
	t.Parallel()
	byAddress := NewRateLimiter(2, time.Hour)
	byEmail := NewRateLimiter(100, time.Hour)

	router := gin.New()
	group := router.Group("")
	group.Use(ThrottleAuth(byAddress, byEmail)...)
	group.POST("/auth/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	codes := make([]int, 0, 3)
	for i, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/auth/login",
			bytes.NewReader([]byte(`{"email":"`+email+`","password":"x"}`)))
		request.RemoteAddr = "203.0.113.7:44321"
		router.ServeHTTP(recorder, request)
		codes = append(codes, recorder.Code)
		_ = i
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Errorf("credential stuffing from one address was not throttled: %v", codes)
	}
}

// The memory cost of being attacked must not scale with the attack.
func TestLimiterEvictsIdleBuckets(t *testing.T) {
	t.Parallel()
	limiter := NewRateLimiter(5, time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := 0; i < 500; i++ {
		limiter.allow("ip:198.51.100." + string(rune('0'+i%10)) + "-" + time.Duration(i).String())
	}
	before := len(limiter.items)
	if before == 0 {
		t.Fatal("no buckets were created")
	}

	// Past the window and past the sweep interval: every bucket has refilled, so every bucket is
	// droppable — a full bucket and no bucket are the same thing to the next caller.
	now = now.Add(2 * time.Minute)
	limiter.allow("ip:203.0.113.1")

	if len(limiter.items) >= before {
		t.Errorf("bucket count went %d → %d; idle buckets are not being evicted", before, len(limiter.items))
	}
}
