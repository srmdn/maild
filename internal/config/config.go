package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppEnv            string
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	ShutdownTimeout   time.Duration

	PostgresDSN string
	RedisAddr   string
	RedisDB     int
}

func Load() Config {
	return Config{
		AppEnv:            getEnv("APP_ENV", "development"),
		Addr:              getEnv("APP_ADDR", ":8080"),
		ReadHeaderTimeout: getDurationEnv("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       getDurationEnv("APP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      getDurationEnv("APP_WRITE_TIMEOUT", 15*time.Second),
		ShutdownTimeout:   getDurationEnv("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN:       getEnv("POSTGRES_DSN", "postgres://maild:maild@localhost:5432/maild?sslmode=disable"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisDB:           getIntEnv("REDIS_DB", 0),
	}
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
