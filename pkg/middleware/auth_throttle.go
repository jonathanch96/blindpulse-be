package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

// Throttling the front door.
//
// Two limiters rather than one composite key, because they defend against different attacks and a
// composite would defend against neither. Credential stuffing is many emails from one address, so
// it is caught by counting addresses. Password spraying is one email from many addresses, so it is
// caught by counting emails. A single limiter keyed on the *pair* gives each combination its own
// budget, which is the most permissive option available and stops neither.
//
// Both must pass. That is the point.

// ClientKey counts by client address. gin's ClientIP already honours the configured trusted-proxy
// settings, so behind a load balancer this is the real caller rather than the balancer.
func ClientKey(c *gin.Context) string {
	return "ip:" + c.ClientIP()
}

// maxCredentialBody caps how much of a login body is read to find the email. A login payload is a
// few hundred bytes; anything larger is not one, and reading it would let an attacker choose how
// much memory each rejected request costs.
const maxCredentialBody = 4 << 10

// EmailKey counts by the email being attempted, so one account cannot be sprayed from many
// addresses. It reads the body and puts it back, because the handler downstream still has to bind
// it — a middleware that consumed the body would turn every login into a validation error.
//
// A request with no readable email falls back to the client address. That is deliberate: an
// unparseable body should be throttled like everything else rather than skipping the limiter, which
// would make "send garbage" the way around it.
func EmailKey(c *gin.Context) string {
	if c.Request.Body == nil {
		return ClientKey(c)
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCredentialBody))
	// Restore it whatever happened, so the handler sees the body it would have seen.
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return ClientKey(c)
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ClientKey(c)
	}
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if email == "" {
		return ClientKey(c)
	}
	return "email:" + email
}

// ThrottleAuth applies both limiters to a route group.
//
// Ordering matters for what an attacker learns: the address limiter runs first, so an attacker
// hammering one address is rejected before the body is read at all.
func ThrottleAuth(byAddress, byEmail *RateLimiter) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		RateLimit(byAddress, ClientKey),
		RateLimit(byEmail, EmailKey),
	}
}
