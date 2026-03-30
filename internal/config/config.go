package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                    string
	Addr                      string
	ReadHeaderTimeout         time.Duration
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	ShutdownTimeout           time.Duration
	MaxAttempts               int
	APIKeyHeader              string
	AdminAPIKey               string
	OperatorAPIKey            string
	EncryptionKeyB64          string
	RateLimitWorkspacePerHour int
	RateLimitDomainPerHour    int
	BlockedRecipientDomains   map[string]struct{}
	WebhooksEnabled           bool
	WebhookSigningSecret      string
	WebhookSignatureHeader    string
	WebhookTimestampHeader    string
	WebhookMaxSkew            time.Duration
	WebhookApplyMaxAttempts   int

	PostgresDSN string
	RedisAddr   string
	RedisDB     int

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

func Load() Config {
	return Config{
		AppEnv:                    getEnv("APP_ENV", "development"),
		Addr:                      getEnv("APP_ADDR", ":8080"),
		ReadHeaderTimeout:         getDurationEnv("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:               getDurationEnv("APP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:              getDurationEnv("APP_WRITE_TIMEOUT", 15*time.Second),
		ShutdownTimeout:           getDurationEnv("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxAttempts:               getIntEnv("APP_MAX_ATTEMPTS", 3),
		APIKeyHeader:              getEnv("API_KEY_HEADER", "X-API-Key"),
		AdminAPIKey:               getEnv("ADMIN_API_KEY", "change-me-admin"),
		OperatorAPIKey:            getEnv("OPERATOR_API_KEY", "change-me-operator"),
		EncryptionKeyB64:          getEnv("ENCRYPTION_KEY_BASE64", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="),
		RateLimitWorkspacePerHour: getIntEnv("RATE_LIMIT_WORKSPACE_PER_HOUR", 400),
		RateLimitDomainPerHour:    getIntEnv("RATE_LIMIT_DOMAIN_PER_HOUR", 200),
		BlockedRecipientDomains:   parseDomainSet(getEnv("BLOCKED_RECIPIENT_DOMAINS", "mailinator.com,tempmail.com")),
		WebhooksEnabled:           getBoolEnv("WEBHOOKS_ENABLED", false),
		WebhookSigningSecret:      getEnv("WEBHOOK_SIGNING_SECRET", ""),
		WebhookSignatureHeader:    getEnv("WEBHOOK_SIGNATURE_HEADER", "X-Webhook-Signature"),
		WebhookTimestampHeader:    getEnv("WEBHOOK_TIMESTAMP_HEADER", "X-Webhook-Timestamp"),
		WebhookMaxSkew:            getDurationEnv("WEBHOOK_MAX_SKEW", 5*time.Minute),
		WebhookApplyMaxAttempts:   getIntEnv("WEBHOOK_APPLY_MAX_ATTEMPTS", 3),
		PostgresDSN:               getEnv("POSTGRES_DSN", "postgres://maild:maild@localhost:5432/maild?sslmode=disable"),
		RedisAddr:                 getEnv("REDIS_ADDR", "localhost:6379"),
		RedisDB:                   getIntEnv("REDIS_DB", 0),
		SMTPHost:                  getEnv("SMTP_HOST", "localhost"),
		SMTPPort:                  getIntEnv("SMTP_PORT", 1025),
		SMTPUsername:              getEnv("SMTP_USERNAME", ""),
		SMTPPassword:              getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:                  getEnv("SMTP_FROM", "noreply@maild.local"),
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.APIKeyHeader) == "" {
		return ErrInvalidConfig("API_KEY_HEADER must not be empty")
	}
	if strings.TrimSpace(c.AdminAPIKey) == "" {
		return ErrInvalidConfig("ADMIN_API_KEY must not be empty")
	}
	if strings.TrimSpace(c.OperatorAPIKey) == "" {
		return ErrInvalidConfig("OPERATOR_API_KEY must not be empty")
	}
	if c.AdminAPIKey == c.OperatorAPIKey {
		return ErrInvalidConfig("ADMIN_API_KEY and OPERATOR_API_KEY must be different")
	}
	if strings.TrimSpace(c.EncryptionKeyB64) == "" {
		return ErrInvalidConfig("ENCRYPTION_KEY_BASE64 must not be empty")
	}
	if c.RateLimitWorkspacePerHour < 1 {
		return ErrInvalidConfig("RATE_LIMIT_WORKSPACE_PER_HOUR must be >= 1")
	}
	if c.RateLimitDomainPerHour < 1 {
		return ErrInvalidConfig("RATE_LIMIT_DOMAIN_PER_HOUR must be >= 1")
	}
	if c.WebhooksEnabled {
		if strings.TrimSpace(c.WebhookSigningSecret) == "" {
			return ErrInvalidConfig("WEBHOOK_SIGNING_SECRET must not be empty when WEBHOOKS_ENABLED=true")
		}
		if strings.TrimSpace(c.WebhookSignatureHeader) == "" {
			return ErrInvalidConfig("WEBHOOK_SIGNATURE_HEADER must not be empty when WEBHOOKS_ENABLED=true")
		}
		if strings.TrimSpace(c.WebhookTimestampHeader) == "" {
			return ErrInvalidConfig("WEBHOOK_TIMESTAMP_HEADER must not be empty when WEBHOOKS_ENABLED=true")
		}
		if c.WebhookMaxSkew <= 0 {
			return ErrInvalidConfig("WEBHOOK_MAX_SKEW must be > 0 when WEBHOOKS_ENABLED=true")
		}
		if c.WebhookApplyMaxAttempts < 1 {
			return ErrInvalidConfig("WEBHOOK_APPLY_MAX_ATTEMPTS must be >= 1 when WEBHOOKS_ENABLED=true")
		}
	}
	return nil
}

type ErrInvalidConfig string

func (e ErrInvalidConfig) Error() string {
	return string(e)
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func getIntEnv(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getBoolEnv(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseDomainSet(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, p := range strings.Split(raw, ",") {
		d := strings.ToLower(strings.TrimSpace(p))
		if d == "" {
			continue
		}
		out[d] = struct{}{}
	}
	return out
}
