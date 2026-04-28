package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	appEnvDevelopment = "development"
	appEnvProduction  = "production"

	defaultAPIKeyHeader     = "X-API-Key"
	defaultAdminAPIKey      = "change-me-admin"
	defaultOperatorAPIKey   = "change-me-operator"
	defaultEncryptionKeyB64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	defaultPostgresDSN      = "postgres://maild:maild@localhost:5432/maild?sslmode=disable"
	defaultRedisAddr        = "localhost:6379"
	defaultSMTPHost         = "localhost"
	defaultSMTPPort         = 1025
	defaultSMTPFrom         = "noreply@maild.local"
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
	AutoFailoverEnabled       bool
	AutoFailoverFailures      int
	AutoFailoverWindow        time.Duration
	AutoFailoverCooldown      time.Duration

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
	appEnv := normalizeAppEnv(getEnv("APP_ENV", appEnvDevelopment))
	isProduction := appEnv == appEnvProduction

	return Config{
		AppEnv:                    appEnv,
		Addr:                      getEnv("APP_ADDR", ":8080"),
		ReadHeaderTimeout:         getDurationEnv("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:               getDurationEnv("APP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:              getDurationEnv("APP_WRITE_TIMEOUT", 15*time.Second),
		ShutdownTimeout:           getDurationEnv("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxAttempts:               getIntEnv("APP_MAX_ATTEMPTS", 3),
		APIKeyHeader:              getEnv("API_KEY_HEADER", defaultAPIKeyHeader),
		AdminAPIKey:               getEnv("ADMIN_API_KEY", envFallbackString(isProduction, defaultAdminAPIKey)),
		OperatorAPIKey:            getEnv("OPERATOR_API_KEY", envFallbackString(isProduction, defaultOperatorAPIKey)),
		EncryptionKeyB64:          getEnv("ENCRYPTION_KEY_BASE64", envFallbackString(isProduction, defaultEncryptionKeyB64)),
		RateLimitWorkspacePerHour: getIntEnv("RATE_LIMIT_WORKSPACE_PER_HOUR", 400),
		RateLimitDomainPerHour:    getIntEnv("RATE_LIMIT_DOMAIN_PER_HOUR", 200),
		BlockedRecipientDomains:   parseDomainSet(getEnv("BLOCKED_RECIPIENT_DOMAINS", "mailinator.com,tempmail.com")),
		WebhooksEnabled:           getBoolEnv("WEBHOOKS_ENABLED", false),
		WebhookSigningSecret:      getEnv("WEBHOOK_SIGNING_SECRET", ""),
		WebhookSignatureHeader:    getEnv("WEBHOOK_SIGNATURE_HEADER", "X-Webhook-Signature"),
		WebhookTimestampHeader:    getEnv("WEBHOOK_TIMESTAMP_HEADER", "X-Webhook-Timestamp"),
		WebhookMaxSkew:            getDurationEnv("WEBHOOK_MAX_SKEW", 5*time.Minute),
		WebhookApplyMaxAttempts:   getIntEnv("WEBHOOK_APPLY_MAX_ATTEMPTS", 3),
		AutoFailoverEnabled:       getBoolEnv("AUTO_FAILOVER_ENABLED", true),
		AutoFailoverFailures:      getIntEnv("AUTO_FAILOVER_FAILURE_THRESHOLD", 3),
		AutoFailoverWindow:        getDurationEnv("AUTO_FAILOVER_WINDOW", 5*time.Minute),
		AutoFailoverCooldown:      getDurationEnv("AUTO_FAILOVER_COOLDOWN", 2*time.Minute),
		PostgresDSN:               getEnv("POSTGRES_DSN", envFallbackString(isProduction, defaultPostgresDSN)),
		RedisAddr:                 getEnv("REDIS_ADDR", envFallbackString(isProduction, defaultRedisAddr)),
		RedisDB:                   getIntEnv("REDIS_DB", 0),
		SMTPHost:                  getEnv("SMTP_HOST", envFallbackString(isProduction, defaultSMTPHost)),
		SMTPPort:                  getIntEnv("SMTP_PORT", envFallbackInt(isProduction, defaultSMTPPort)),
		SMTPUsername:              getEnv("SMTP_USERNAME", ""),
		SMTPPassword:              getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:                  getEnv("SMTP_FROM", envFallbackString(isProduction, defaultSMTPFrom)),
	}
}

func (c Config) Validate() error {
	appEnv := normalizeAppEnv(c.AppEnv)
	if appEnv == "" {
		return ErrInvalidConfig("APP_ENV must not be empty")
	}
	if appEnv != appEnvDevelopment && appEnv != appEnvProduction {
		return ErrInvalidConfig("APP_ENV must be one of: development, production")
	}
	if appEnv == appEnvProduction {
		if err := validateProductionConfig(c); err != nil {
			return err
		}
	}

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
	if strings.TrimSpace(c.PostgresDSN) == "" {
		return ErrInvalidConfig("POSTGRES_DSN must not be empty")
	}
	if strings.TrimSpace(c.RedisAddr) == "" {
		return ErrInvalidConfig("REDIS_ADDR must not be empty")
	}
	if strings.TrimSpace(c.SMTPHost) == "" {
		return ErrInvalidConfig("SMTP_HOST must not be empty")
	}
	if c.SMTPPort < 1 {
		return ErrInvalidConfig("SMTP_PORT must be >= 1")
	}
	if strings.TrimSpace(c.SMTPFrom) == "" {
		return ErrInvalidConfig("SMTP_FROM must not be empty")
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
	if c.AutoFailoverEnabled {
		if c.AutoFailoverFailures < 1 {
			return ErrInvalidConfig("AUTO_FAILOVER_FAILURE_THRESHOLD must be >= 1 when AUTO_FAILOVER_ENABLED=true")
		}
		if c.AutoFailoverWindow <= 0 {
			return ErrInvalidConfig("AUTO_FAILOVER_WINDOW must be > 0 when AUTO_FAILOVER_ENABLED=true")
		}
		if c.AutoFailoverCooldown < 0 {
			return ErrInvalidConfig("AUTO_FAILOVER_COOLDOWN must be >= 0 when AUTO_FAILOVER_ENABLED=true")
		}
	}
	return nil
}

type ErrInvalidConfig string

func (e ErrInvalidConfig) Error() string {
	return string(e)
}

func validateProductionConfig(c Config) error {
	if strings.TrimSpace(c.AdminAPIKey) == "" {
		return ErrInvalidConfig("ADMIN_API_KEY is required when APP_ENV=production")
	}
	if strings.TrimSpace(c.OperatorAPIKey) == "" {
		return ErrInvalidConfig("OPERATOR_API_KEY is required when APP_ENV=production")
	}
	if strings.TrimSpace(c.EncryptionKeyB64) == "" {
		return ErrInvalidConfig("ENCRYPTION_KEY_BASE64 is required when APP_ENV=production")
	}
	if strings.TrimSpace(c.PostgresDSN) == "" {
		return ErrInvalidConfig("POSTGRES_DSN is required when APP_ENV=production")
	}
	if strings.TrimSpace(c.RedisAddr) == "" {
		return ErrInvalidConfig("REDIS_ADDR is required when APP_ENV=production")
	}
	if strings.TrimSpace(c.SMTPHost) == "" {
		return ErrInvalidConfig("SMTP_HOST is required when APP_ENV=production")
	}
	if c.SMTPPort < 1 {
		return ErrInvalidConfig("SMTP_PORT must be >= 1 when APP_ENV=production")
	}
	if strings.TrimSpace(c.SMTPFrom) == "" {
		return ErrInvalidConfig("SMTP_FROM is required when APP_ENV=production")
	}

	if c.AdminAPIKey == defaultAdminAPIKey {
		return ErrInvalidConfig("ADMIN_API_KEY must not use development default when APP_ENV=production")
	}
	if c.OperatorAPIKey == defaultOperatorAPIKey {
		return ErrInvalidConfig("OPERATOR_API_KEY must not use development default when APP_ENV=production")
	}
	if c.EncryptionKeyB64 == defaultEncryptionKeyB64 {
		return ErrInvalidConfig("ENCRYPTION_KEY_BASE64 must not use development default when APP_ENV=production")
	}
	if c.PostgresDSN == defaultPostgresDSN {
		return ErrInvalidConfig("POSTGRES_DSN must not use development default when APP_ENV=production")
	}
	if c.RedisAddr == defaultRedisAddr {
		return ErrInvalidConfig("REDIS_ADDR must not use development default when APP_ENV=production")
	}
	if c.SMTPHost == defaultSMTPHost {
		return ErrInvalidConfig("SMTP_HOST must not use development default when APP_ENV=production")
	}
	if c.SMTPPort == defaultSMTPPort {
		return ErrInvalidConfig("SMTP_PORT must not use development default when APP_ENV=production")
	}
	if c.SMTPFrom == defaultSMTPFrom {
		return ErrInvalidConfig("SMTP_FROM must not use development default when APP_ENV=production")
	}

	return nil
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func envFallbackString(isProduction bool, developmentFallback string) string {
	if isProduction {
		return ""
	}
	return developmentFallback
}

func envFallbackInt(isProduction bool, developmentFallback int) int {
	if isProduction {
		return 0
	}
	return developmentFallback
}

func normalizeAppEnv(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
