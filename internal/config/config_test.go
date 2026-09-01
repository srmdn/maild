package config

import (
	"encoding/base64"
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
	"APP_ALLOW_SIGNUP",
	"APP_LOGIN_PATH",
	"APP_LOGIN_MAX_FAILURES",
	"APP_LOGIN_FAILURE_WINDOW",
	"APP_LOGIN_LOCKOUT",
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

func TestValidateRejectsBadLoginPath(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "development")

	cases := []struct {
		name string
		path string
		want string
	}{
		{"no-leading-slash", "login", "APP_LOGIN_PATH must start with '/'"},
		{"reserved", "/signup", "APP_LOGIN_PATH conflicts with a reserved route"},
		{"whitespace", "/a b", "must not contain spaces, '?', '#', or tabs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_LOGIN_PATH", tc.path)
			cfg := Load()
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error for login path %q, got nil", tc.path)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

func TestValidateRejectsBadLoginRateLimit(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_LOGIN_MAX_FAILURES", "0")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for zero login max failures, got nil")
	}
	if !strings.Contains(err.Error(), "APP_LOGIN_MAX_FAILURES must be >= 1") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoginDefaults(t *testing.T) {
	resetConfigEnv(t)
	t.Setenv("APP_ENV", "development")

	cfg := Load()
	if !cfg.AllowSignup {
		t.Fatal("expected APP_ALLOW_SIGNUP to default to true")
	}
	if cfg.LoginPath != "/login" {
		t.Fatalf("expected APP_LOGIN_PATH to default to /login, got %q", cfg.LoginPath)
	}
	if cfg.LoginMaxFailures != 5 {
		t.Fatalf("expected APP_LOGIN_MAX_FAILURES to default to 5, got %d", cfg.LoginMaxFailures)
	}
	if cfg.LoginLockout <= 0 || cfg.LoginFailureWindow <= 0 {
		t.Fatal("expected positive login rate-limit durations by default")
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
	t.Setenv("ADMIN_API_KEY", "admin-key")
	t.Setenv("OPERATOR_API_KEY", "operator-key")
	t.Setenv("ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("prod-test-encryption-key-0000000")))
	t.Setenv("POSTGRES_DSN", "postgres://maild:supersecure@db.internal:5432/maild?sslmode=require")
	t.Setenv("REDIS_ADDR", "redis.internal:6379")
	t.Setenv("SMTP_HOST", "smtp.internal")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_FROM", "noreply@example.com")
}
