package controllers

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	apphash "github.com/jblabs/blindpulse-be/pkg/hash"
	appjwt "github.com/jblabs/blindpulse-be/pkg/jwt"
	"github.com/jblabs/blindpulse-be/pkg/middleware"
	googleoauth "github.com/jblabs/blindpulse-be/pkg/oauth/google"
	"github.com/jblabs/blindpulse-be/pkg/response"
	accountcontroller "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers/account"
	authcontroller "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers/auth"
	feedcontroller "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers/feed"
	sessioncontroller "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers/session"
	usercontroller "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers/user"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	ledgerdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/account_ledger_entries"
	accountsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/accounts"
	feedsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/blinded_feeds"
	instrumentsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/instruments"
	barsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/market_bars"
	outboxdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/outbox_events"
	refreshtokens "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/refresh_tokens"
	sessionsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/replay_sessions"
	users "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/users"
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Dependencies struct {
	DB    *gorm.DB
	Cfg   *config.Config
	Log   *slog.Logger
	Cache *cache.Client
}

type Service struct {
	auth     authcontroller.Controller
	users    usercontroller.Controller
	accounts accountcontroller.Controller
	feeds    feedcontroller.Controller
	sessions sessioncontroller.Controller
	issuer   *appjwt.Issuer
}

func NewService(deps Dependencies) *Service {
	issuer := appjwt.NewIssuer(appjwt.Config{
		AccessSecret: deps.Cfg.JWT.AccessSecret, RefreshSecret: deps.Cfg.JWT.RefreshSecret,
		AccessTTL: deps.Cfg.JWT.AccessTTL, RefreshTTL: deps.Cfg.JWT.RefreshTTL,
	})
	var googleVerifier userdomain.GoogleVerifier
	if deps.Cfg.Google.ClientID != "" {
		googleVerifier = googleVerifierAdapter{v: googleoauth.NewVerifier(deps.Cfg.Google.ClientID)}
	}
	userService := userdomain.NewService(userdomain.Dependencies{
		Repo:   users.NewGormPostgresqlAdapter(deps.DB),
		Tokens: refreshtokens.NewGormPostgresqlAdapter(deps.DB),
		Hasher: apphash.NewArgon2Hasher(), Issuer: issuer, Google: googleVerifier,
	})
	accountService := accountdomain.NewService(accountdomain.Dependencies{
		Repo:   accountsdb.New(deps.DB),
		Ledger: ledgerdb.New(deps.DB),
		Outbox: outboxdb.New(deps.DB),
		UOW:    appdb.NewGormUnitOfWork(deps.DB),
		// The domain names a logical topic; the composition root is where it becomes an
		// environment-prefixed one, so a domain package never encodes deployment layout.
		Topic: deps.Cfg.Kafka.Topic,
	})
	instrumentRepo := instrumentsdb.New(deps.DB)
	feedService := feeddomain.NewService(feeddomain.Dependencies{
		Repo:        feedsdb.New(deps.DB),
		Bars:        barsdb.New(deps.DB),
		Instruments: instrumentRepo,
	})
	sessionService := sessiondomain.NewService(sessiondomain.Dependencies{
		Repo:  sessionsdb.New(deps.DB),
		State: sessionsdb.NewRedisStateStore(deps.Cache, deps.Cfg.Redis.SessionTTL),
		Feeds: feedService,
		// The session domain reaches accounts through a narrow reader rather than the whole
		// account service: it only needs to know the account exists, is the caller's, and is live.
		Accounts:          sessiondomain.NewAccountReader(accountsdb.New(deps.DB)),
		Outbox:            outboxdb.New(deps.DB),
		Topic:             deps.Cfg.Kafka.Topic,
		MinSpeed:          decimal.NewFromFloat(deps.Cfg.Replay.MinSpeed),
		MaxSpeed:          decimal.NewFromFloat(deps.Cfg.Replay.MaxSpeed),
		MaxBarsPerRequest: deps.Cfg.Replay.BarWindowSize,
	})
	return &Service{
		auth:     authcontroller.NewController(userService),
		users:    usercontroller.NewController(userService),
		accounts: accountcontroller.NewController(accountService),
		feeds:    feedcontroller.NewController(feedService, instrumentRepo),
		sessions: sessioncontroller.NewController(sessionService, feedService),
		issuer:   issuer,
	}
}

func (s *Service) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/ping", s.Ping)
	s.auth.RegisterRoutes(group)
	protected := group.Group("")
	protected.Use(middleware.Authenticate(s.issuer))
	s.users.RegisterRoutes(protected)
	s.accounts.RegisterRoutes(protected)
	s.feeds.RegisterRoutes(protected)
	s.sessions.RegisterRoutes(protected)
}

// googleVerifierAdapter adapts pkg/oauth/google's Verifier (which returns its own Claims type) to
// the domain/user package's GoogleVerifier interface, so the domain layer doesn't need to import
// an infrastructure package just to describe the shape of a verified Google identity.
type googleVerifierAdapter struct{ v *googleoauth.Verifier }

func (a googleVerifierAdapter) Verify(ctx context.Context, idToken string) (*userdomain.GoogleClaims, error) {
	claims, err := a.v.Verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	return &userdomain.GoogleClaims{Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name}, nil
}

// Ping godoc
// @Summary Check API connectivity
// @Description Returns a fully populated envelope proving the API composition root is wired.
// @Tags system
// @Produce json
// @Success 200 {object} response.Envelope
// @Failure 500 {object} response.Envelope
// @Router /ping [get]
func (s *Service) Ping(c *gin.Context) {
	response.OK(c, "PONG", gin.H{"pong": true})
}
