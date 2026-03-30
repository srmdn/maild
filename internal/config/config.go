package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv            string
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	ShutdownTimeout   time.Duration
	MaxAttempts       int
	APIKeyHeader      string
	APIKey            string

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
		AppEnv:            getEnv("APP_ENV", "development"),
		Addr:              getEnv("APP_ADDR", ":8080"),
		ReadHeaderTimeout: getDurationEnv("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       getDurationEnv("APP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      getDurationEnv("APP_WRITE_TIMEOUT", 15*time.Second),
		ShutdownTimeout:   getDurationEnv("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxAttempts:       getIntEnv("APP_MAX_ATTEMPTS", 3),
		APIKeyHeader:      getEnv("API_KEY_HEADER", "X-API-Key"),
		APIKey:            getEnv("API_KEY", "change-me"),
		PostgresDSN:       getEnv("POSTGRES_DSN", "postgres://maild:maild@localhost:5432/maild?sslmode=disable"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisDB:           getIntEnv("REDIS_DB", 0),
		SMTPHost:          getEnv("SMTP_HOST", "localhost"),
		SMTPPort:          getIntEnv("SMTP_PORT", 1025),
		SMTPUsername:      getEnv("SMTP_USERNAME", ""),
		SMTPPassword:      getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:          getEnv("SMTP_FROM", "noreply@maild.local"),
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.APIKeyHeader) == "" {
		return ErrInvalidConfig("API_KEY_HEADER must not be empty")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return ErrInvalidConfig("API_KEY must not be empty")
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
