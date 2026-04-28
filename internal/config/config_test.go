package config

import (
	"strings"
	"testing"
)

var configEnvKeys = []string{
	"APP_ENV",
	"APP_ADDR",
	"APP_READ_HEADER_TIMEOUT",
	"APP_READ_TIMEOUT",
	"APP_WRITE_TIMEOUT",
	"APP_SHUTDOWN_TIMEOUT",
	"APP_MAX_ATTEMPTS",
	"API_KEY_HEADER",
	"ADMIN_API_KEY",
	"OPERATOR_API_KEY",
	"ENCRYPTION_KEY_BASE64",
	"RATE_LIMIT_WORKSPACE_PER_HOUR",
	"RATE_LIMIT_DOMAIN_PER_HOUR",
	"BLOCKED_RECIPIENT_DOMAINS",
	"WEBHOOKS_ENABLED",
	"WEBHOOK_SIGNING_SECRET",
	"WEBHOOK_SIGNATURE_HEADER",
	"WEBHOOK_TIMESTAMP_HEADER",
	"WEBHOOK_MAX_SKEW",
	"WEBHOOK_APPLY_MAX_ATTEMPTS",
	"AUTO_FAILOVER_ENABLED",
	"AUTO_FAILOVER_FAILURE_THRESHOLD",
	"AUTO_FAILOVER_WINDOW",
	"AUTO_FAILOVER_COOLDOWN",
	"POSTGRES_DSN",
	"REDIS_ADDR",
	"REDIS_DB",
	"SMTP_HOST",
	"SMTP_PORT",
	"SMTP_USERNAME",
	"SMTP_PASSWORD",
	"SMTP_FROM",
}

func TestValidateDevelopmentDefaultsPass(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "development")

	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected development defaults to pass validation, got error: %v", err)
	}
}

func TestValidateRejectsUnknownAppEnv(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "staging")
	setValidProductionRuntimeEnv(t)

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for unknown APP_ENV, got nil")
	}
	if !strings.Contains(err.Error(), "APP_ENV must be one of: development, production") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateProductionRequiresExplicitSecretsAndRuntime(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "production")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected production validation to fail for missing required values, got nil")
	}
	if !strings.Contains(err.Error(), "ADMIN_API_KEY is required when APP_ENV=production") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateProductionRejectsDevelopmentDefaults(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidProductionRuntimeEnv(t)
	t.Setenv("ADMIN_API_KEY", defaultAdminAPIKey)

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected production validation to reject development default, got nil")
	}
	if !strings.Contains(err.Error(), "ADMIN_API_KEY must not use development default when APP_ENV=production") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateProductionValidConfigPass(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	setValidProductionRuntimeEnv(t)

	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid production configuration to pass, got error: %v", err)
	}
}

func resetConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

func setValidProductionRuntimeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ADMIN_API_KEY", "prod-admin-key-123")
	t.Setenv("OPERATOR_API_KEY", "prod-operator-key-456")
	t.Setenv("ENCRYPTION_KEY_BASE64", "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo0NTY3ODkwMTI=")
	t.Setenv("POSTGRES_DSN", "postgres://maild:supersecure@db.internal:5432/maild?sslmode=require")
	t.Setenv("REDIS_ADDR", "redis.internal:6379")
	t.Setenv("SMTP_HOST", "smtp.internal")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "noreply@example.com")
}
