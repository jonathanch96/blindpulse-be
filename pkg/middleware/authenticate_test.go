package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	appjwt "github.com/jblabs/blindpulse-be/pkg/jwt"
	"github.com/jblabs/blindpulse-be/pkg/response"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func authIssuer(ttl time.Duration) *appjwt.Issuer {
	return appjwt.NewIssuer(appjwt.Config{AccessSecret: strings.Repeat("a", 32), RefreshSecret: strings.Repeat("b", 32), AccessTTL: ttl, RefreshTTL: time.Hour})
}

func TestAuthenticateMissingMalformedAndExpired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct{ name, header string }{{"missing", ""}, {"malformed", "Basic abc"}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(Authenticate(authIssuer(time.Minute)))
			engine.GET("/", func(c *gin.Context) { c.Status(204) })
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Authorization", test.header)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != 401 {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
	issuer := authIssuer(-time.Second)
	raw, _, _ := issuer.IssueAccess(domainuser.User{ID: uuid.New()})
	engine := gin.New()
	engine.Use(Authenticate(issuer))
	engine.GET("/", func(c *gin.Context) { c.Status(204) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	var envelope response.Envelope
	_ = json.Unmarshal(recorder.Body.Bytes(), &envelope)
	if recorder.Code != 401 || envelope.Message != "session expired" {
		t.Fatalf("expired response = %d %+v", recorder.Code, envelope)
	}
}

func TestAuthenticateSetsIdentityAndIgnoresUserHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	issuer, userID := authIssuer(time.Minute), uuid.New()
	raw, _, err := issuer.IssueAccess(domainuser.User{ID: userID, Email: "a@example.com", Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(Authenticate(issuer))
	engine.GET("/", func(c *gin.Context) {
		current, ok := identity.FromContext(c.Request.Context())
		if !ok || current.UserID != userID || c.GetHeader("X-User-ID") != "" {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set("X-User-ID", uuid.NewString())
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != 204 {
		t.Fatalf("status = %d", recorder.Code)
	}
}
