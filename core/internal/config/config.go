package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPPort          string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	PostgresDSN       string
	KafkaBrokers      []string
}

func Load() (Config, error) {
	readHeaderTimeout, err := time.ParseDuration(env("HTTP_READ_HEADER_TIMEOUT", "5s"))
	if err != nil {
		return Config{}, fmt.Errorf("parse HTTP_READ_HEADER_TIMEOUT: %w", err)
	}

	shutdownTimeout, err := time.ParseDuration(env("HTTP_SHUTDOWN_TIMEOUT", "10s"))
	if err != nil {
		return Config{}, fmt.Errorf("parse HTTP_SHUTDOWN_TIMEOUT: %w", err)
	}

	brokers := splitAndTrim(env("KAFKA_BROKERS", "localhost:9092"))
	if len(brokers) == 0 {
		brokers = []string{"localhost:9092"}
	}

	return Config{
		HTTPPort:          env("HTTP_PORT", "8080"),
		ReadHeaderTimeout: readHeaderTimeout,
		ShutdownTimeout:   shutdownTimeout,
		PostgresDSN:       postgresDSN(),
		KafkaBrokers:      brokers,
	}, nil
}

func postgresDSN() string {
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}

	host := env("POSTGRES_HOST", "localhost")
	port := env("POSTGRES_PORT", "5432")
	db := env("POSTGRES_DB", "mdm")
	user := env("POSTGRES_USER", "mdm")
	password := env("POSTGRES_PASSWORD", "mdm")

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, db)
}

func splitAndTrim(source string) []string {
	parts := strings.Split(source, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
