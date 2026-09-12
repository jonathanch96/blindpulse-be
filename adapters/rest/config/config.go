package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	App     AppConfig
	DB      DBConfig
	JWT     JWTConfig
	Google  GoogleConfig
	CORS    CORSConfig
	Storage StorageConfig
	Redis   RedisConfig
	Kafka   KafkaConfig
	Replay  ReplayConfig
	Auth    AuthConfig
	// Execution is the simulated venue's frictions.
	Execution ExecutionConfig
}

// ExecutionConfig is how expensive it is to trade against this simulator.
//
// Both default to something rather than zero on purpose. A frictionless fill engine is the single
// most flattering bug a replay tool can have: every strategy looks better without a spread, and a
// trader who learned their edge here would lose it the moment they traded a real book. Set them to 0
// only for a teaching feed where the point is the mechanics rather than the edge.
type ExecutionConfig struct {
	// SpreadTicks is the full bid-ask width; a fill crosses half of it.
	SpreadTicks int `envconfig:"EXECUTION_SPREAD_TICKS" default:"1"`
	// MaxSlippageTicks bounds the adverse-only slippage draw. The draw itself is a hash of the
	// session seed, the bar index and the order's sequence, never a random stream — so a disputed
	// fill can be recomputed from the three coordinates alone (NFR-03).
	MaxSlippageTicks int `envconfig:"EXECUTION_MAX_SLIPPAGE_TICKS" default:"2"`
}

// AuthConfig throttles the front door. Two independent budgets: one per client address, one per
// email being attempted. A composite key would give every (address, email) pair its own budget and
// stop neither credential stuffing nor password spraying.
//
// The defaults are deliberately generous enough that a person fat-fingering their password several
// times is unaffected, and tight enough that unlimited automated attempts are not.
type AuthConfig struct {
	AddressAttempts int           `envconfig:"AUTH_RATE_LIMIT_PER_ADDRESS" default:"30"`
	EmailAttempts   int           `envconfig:"AUTH_RATE_LIMIT_PER_EMAIL" default:"10"`
	Window          time.Duration `envconfig:"AUTH_RATE_LIMIT_WINDOW" default:"15m"`
}

type AppConfig struct {
	Env             string        `envconfig:"APP_ENV" default:"local"`
	Port            int           `envconfig:"APP_PORT" default:"8080"`
	ShutdownTimeout time.Duration `envconfig:"APP_SHUTDOWN_TIMEOUT" default:"15s"`
}

type DBConfig struct {
	Host            string        `envconfig:"DB_HOST" required:"true"`
	Port            int           `envconfig:"DB_PORT" default:"5432"`
	User            string        `envconfig:"DB_USER" required:"true"`
	Password        string        `envconfig:"DB_PASSWORD" required:"true"`
	Name            string        `envconfig:"DB_NAME" required:"true"`
	Schema          string        `envconfig:"DB_SCHEMA" default:"blindpulse"`
	SSLMode         string        `envconfig:"DB_SSLMODE" default:"disable"`
	MaxOpenConns    int           `envconfig:"DB_MAX_OPEN_CONNS" default:"25"`
	MaxIdleConns    int           `envconfig:"DB_MAX_IDLE_CONNS" default:"5"`
	ConnMaxLifetime time.Duration `envconfig:"DB_CONN_MAX_LIFETIME" default:"30m"`
	AutoMigrate     bool          `envconfig:"DB_AUTO_MIGRATE" default:"false"`
}

type JWTConfig struct {
	AccessSecret  string        `envconfig:"JWT_ACCESS_SECRET"`
	RefreshSecret string        `envconfig:"JWT_REFRESH_SECRET"`
	AccessTTL     time.Duration `envconfig:"JWT_ACCESS_TTL" default:"15m"`
	RefreshTTL    time.Duration `envconfig:"JWT_REFRESH_TTL" default:"720h"`
}

type GoogleConfig struct {
	// ClientID is the OAuth client ID the frontend requests Google ID tokens for; it doubles as
	// the expected audience when this service verifies those tokens. Empty disables Google sign-in.
	ClientID string `envconfig:"GOOGLE_CLIENT_ID"`
}

type CORSConfig struct {
	AllowedOrigins []string `envconfig:"CORS_ALLOWED_ORIGINS" default:"http://localhost:3000"`
}

// StorageConfig points at the object store holding journal screenshots and exported session
// reports. Leaving Endpoint empty falls back to signed local-filesystem storage.
type StorageConfig struct {
	Endpoint  string `envconfig:"STORAGE_ENDPOINT"`
	AccessKey string `envconfig:"STORAGE_ACCESS_KEY"`
	SecretKey string `envconfig:"STORAGE_SECRET_KEY"`
	Bucket    string `envconfig:"STORAGE_BUCKET" default:"blindpulse-journal"`
	Region    string `envconfig:"STORAGE_REGION" default:"us-east-1"`
	UseSSL    bool   `envconfig:"STORAGE_USE_SSL" default:"false"`
	LocalRoot string `envconfig:"STORAGE_LOCAL_ROOT" default:"./var/journal"`
	PublicURL string `envconfig:"STORAGE_PUBLIC_URL" default:"http://localhost:8080/api/v1/journal-media"`
	// MaxUploadBytes caps one journal image.
	MaxUploadBytes int64 `envconfig:"STORAGE_MAX_UPLOAD_BYTES" default:"5242880"`
	// URLTTL is how long a signed media link stays valid. Short, because the link is the authority:
	// an <img src> cannot carry a bearer token, so anyone holding the URL can fetch it until it
	// expires. Long enough that a screen stays rendered while a trader reads it.
	URLTTL time.Duration `envconfig:"STORAGE_URL_TTL" default:"15m"`
	// SignSecret signs those links. Empty disables media uploads entirely rather than signing with
	// a known key — a predictable signature is worse than no feature, because it looks like
	// protection. It is separate from the JWT secrets on purpose: a key used for two jobs makes
	// rotating it for one of them a decision about the other.
	SignSecret string `envconfig:"STORAGE_SIGN_SECRET"`
}

// RedisConfig drives the replay hot path: session cursor state, bar-window caches, order
// idempotency keys, distributed rate limiting, and the cross-instance replay pub/sub fan-out.
// An empty Addr disables Redis and degrades those features to in-process equivalents.
type RedisConfig struct {
	Addr         string        `envconfig:"REDIS_ADDR"`
	Password     string        `envconfig:"REDIS_PASSWORD"`
	DB           int           `envconfig:"REDIS_DB" default:"0"`
	Namespace    string        `envconfig:"REDIS_NAMESPACE" default:"blindpulse"`
	DialTimeout  time.Duration `envconfig:"REDIS_DIAL_TIMEOUT" default:"3s"`
	ReadTimeout  time.Duration `envconfig:"REDIS_READ_TIMEOUT" default:"2s"`
	PoolSize     int           `envconfig:"REDIS_POOL_SIZE" default:"20"`
	SessionTTL   time.Duration `envconfig:"REDIS_SESSION_TTL" default:"12h"`
	BarWindowTTL time.Duration `envconfig:"REDIS_BAR_WINDOW_TTL" default:"1h"`
}

// KafkaConfig drives the durable event spine. The outbox relay is the only writer: domain
// transactions append to blindpulse.outbox_events, the relay publishes, and projectors consume.
// An empty Brokers list disables publishing and leaves events queued in the outbox.
type KafkaConfig struct {
	Brokers       []string      `envconfig:"KAFKA_BROKERS"`
	TopicPrefix   string        `envconfig:"KAFKA_TOPIC_PREFIX" default:"blindpulse"`
	ConsumerGroup string        `envconfig:"KAFKA_CONSUMER_GROUP" default:"blindpulse-projectors"`
	ClientID      string        `envconfig:"KAFKA_CLIENT_ID" default:"blindpulse-api"`
	BatchSize     int           `envconfig:"KAFKA_BATCH_SIZE" default:"100"`
	BatchTimeout  time.Duration `envconfig:"KAFKA_BATCH_TIMEOUT" default:"200ms"`
	WriteTimeout  time.Duration `envconfig:"KAFKA_WRITE_TIMEOUT" default:"5s"`
	RelayInterval time.Duration `envconfig:"KAFKA_RELAY_INTERVAL" default:"1s"`
	RelayBatch    int           `envconfig:"KAFKA_RELAY_BATCH" default:"200"`
	MaxAttempts   int           `envconfig:"KAFKA_MAX_ATTEMPTS" default:"8"`
}

// ReplayConfig bounds the simulator. MaxSpeed caps the PRD's 0.5x-10x playback range, and
// MaxOpenPositions/MinRiskReward/MaxDailyDrawdown are the server-side mirrors of the risk gates
// the terminal enforces in the UI - the client's copy is a convenience, this one is authoritative.
type ReplayConfig struct {
	MaxSpeed         float64 `envconfig:"REPLAY_MAX_SPEED" default:"10"`
	MinSpeed         float64 `envconfig:"REPLAY_MIN_SPEED" default:"0.5"`
	MaxOpenPositions int     `envconfig:"REPLAY_MAX_OPEN_POSITIONS" default:"10"`
	MinRiskReward    float64 `envconfig:"REPLAY_MIN_RISK_REWARD" default:"2"`
	MaxDailyDrawdown float64 `envconfig:"REPLAY_MAX_DAILY_DRAWDOWN_PCT" default:"5"`
	BarWindowSize    int     `envconfig:"REPLAY_BAR_WINDOW_SIZE" default:"1500"`
	// StreamTickInterval is how long one bar takes at 1x. This is replay time, not market time:
	// a 15m feed walked at market pace would take a working week, so 1x means one bar per tick
	// and speed divides it.
	StreamTickInterval time.Duration `envconfig:"REPLAY_STREAM_TICK_INTERVAL" default:"250ms"`
	// StreamTicketTTL bounds how long a websocket ticket is worth anything. Long enough for the
	// browser to make one connection, short enough that a ticket in a log is already dead.
	StreamTicketTTL    time.Duration `envconfig:"REPLAY_STREAM_TICKET_TTL" default:"30s"`
	SessionIdleTimeout time.Duration `envconfig:"REPLAY_SESSION_IDLE_TIMEOUT" default:"30m"`
	// How often the worker looks for idle sessions. Deliberately much coarser than the timeout:
	// nothing depends on abandoning a session promptly, only on abandoning it eventually.
	SessionSweepInterval time.Duration `envconfig:"REPLAY_SESSION_SWEEP_INTERVAL" default:"5m"`
}

func Load() (*Config, error) {
	environment := os.Getenv("APP_ENV")
	if environment == "" || environment == "local" || environment == "test" {
		_ = godotenv.Load()
	}
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	validEnvironments := map[string]bool{"local": true, "test": true, "staging": true, "production": true}
	if !validEnvironments[c.App.Env] {
		return fmt.Errorf("APP_ENV must be local, test, staging, or production")
	}
	if c.App.Port < 1 || c.App.Port > 65535 {
		return fmt.Errorf("APP_PORT must be between 1 and 65535")
	}
	if c.App.ShutdownTimeout <= 0 {
		return fmt.Errorf("APP_SHUTDOWN_TIMEOUT must be positive")
	}
	if c.DB.MaxOpenConns < 1 || c.DB.MaxIdleConns < 0 || c.DB.MaxIdleConns > c.DB.MaxOpenConns {
		return fmt.Errorf("database pool settings are invalid")
	}
	if c.DB.Schema == "" || strings.ContainsAny(c.DB.Schema, " ;,'\"") {
		return fmt.Errorf("DB_SCHEMA is invalid")
	}
	if c.DB.AutoMigrate && c.App.Env != "local" && c.App.Env != "test" {
		return fmt.Errorf("DB_AUTO_MIGRATE can only be enabled in local or test environments")
	}
	if len(c.JWT.AccessSecret) < 32 || len(c.JWT.RefreshSecret) < 32 {
		return fmt.Errorf("JWT secrets must be at least 32 bytes")
	}
	if c.Storage.Endpoint != "" && (c.Storage.AccessKey == "" || c.Storage.SecretKey == "") {
		return fmt.Errorf("STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY are required with STORAGE_ENDPOINT")
	}
	if c.Redis.PoolSize < 1 {
		return fmt.Errorf("REDIS_POOL_SIZE must be positive")
	}
	if c.Kafka.RelayBatch < 1 || c.Kafka.MaxAttempts < 1 {
		return fmt.Errorf("KAFKA_RELAY_BATCH and KAFKA_MAX_ATTEMPTS must be positive")
	}
	if c.Kafka.TopicPrefix == "" || strings.ContainsAny(c.Kafka.TopicPrefix, " ,") {
		return fmt.Errorf("KAFKA_TOPIC_PREFIX is invalid")
	}
	if c.Auth.AddressAttempts < 1 || c.Auth.EmailAttempts < 1 || c.Auth.Window <= 0 {
		return fmt.Errorf("AUTH_RATE_LIMIT_* must be positive")
	}
	if c.Replay.MinSpeed <= 0 || c.Replay.MaxSpeed < c.Replay.MinSpeed {
		return fmt.Errorf("REPLAY_MIN_SPEED and REPLAY_MAX_SPEED are invalid")
	}
	if c.Replay.MaxDailyDrawdown <= 0 || c.Replay.MaxDailyDrawdown > 100 {
		return fmt.Errorf("REPLAY_MAX_DAILY_DRAWDOWN_PCT must be between 0 and 100")
	}
	if c.Replay.BarWindowSize < 1 {
		return fmt.Errorf("REPLAY_BAR_WINDOW_SIZE must be positive")
	}
	if c.IsProduction() {
		for _, origin := range c.CORS.AllowedOrigins {
			if strings.TrimSpace(origin) == "*" {
				return fmt.Errorf("CORS_ALLOWED_ORIGINS cannot contain * in production")
			}
		}
	}
	return nil
}

func (c *Config) IsProduction() bool { return c.App.Env == "production" }

// RedisEnabled reports whether the replay hot path has a shared store. Callers degrade instead of
// failing: a single instance can serve replay from process memory, it just cannot scale out.
func (c *Config) RedisEnabled() bool { return strings.TrimSpace(c.Redis.Addr) != "" }

// KafkaEnabled reports whether the outbox relay has somewhere to publish. When false, domain
// writes still record their events - nothing is lost, publication simply waits for a broker.
func (c *Config) KafkaEnabled() bool { return len(c.Kafka.Brokers) > 0 }

func (c DBConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s search_path=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode, c.Schema)
}

func (c DBConfig) MigrationURL() string {
	u := &url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password), Host: fmt.Sprintf("%s:%d", c.Host, c.Port), Path: c.Name}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	q.Set("search_path", "public")
	u.RawQuery = q.Encode()
	return u.String()
}

// Topic namespaces every event stream under the configured prefix so several environments can
// share one broker cluster without colliding.
func (c KafkaConfig) Topic(name string) string { return c.TopicPrefix + "." + name }

// Key namespaces every Redis key the same way, for the same reason.
func (c RedisConfig) Key(parts ...string) string {
	return c.Namespace + ":" + strings.Join(parts, ":")
}
