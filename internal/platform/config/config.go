package config

import (
	"fmt"
	"os"
	"strings"
)

type PlatformConfig struct {
	Port           string
	DatabaseURL    string
	AdminToken     string
	K8sNamespace   string
	OperatorAPIURL string
	CORS           CORSConfig
}

type CORSConfig struct {
	AllowedOrigins []string
}

func Load() (*PlatformConfig, error) {
	cfg := &PlatformConfig{
		Port:           envOr("PORT", "8090"),
		DatabaseURL:    buildDatabaseURL(),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		K8sNamespace:   envOr("K8S_NAMESPACE", "reposhift"),
		OperatorAPIURL: envOr("OPERATOR_API_URL", "http://reposhift-operator:8080"),
		CORS: CORSConfig{
			AllowedOrigins: splitEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3005"),
		},
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *PlatformConfig) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("database connection is required (set POSTGRES_HOST or DATABASE_URL)")
	}
	if c.AdminToken == "" {
		return fmt.Errorf("ADMIN_TOKEN is required")
	}
	if len(c.AdminToken) < 16 {
		return fmt.Errorf("ADMIN_TOKEN must be at least 16 characters")
	}
	return nil
}

func buildDatabaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	host := envOr("POSTGRES_HOST", "localhost")
	port := envOr("POSTGRES_PORT", "5432")
	user := envOr("POSTGRES_USER", "postgres")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := envOr("POSTGRES_DATABASE", "reposhift_db")
	sslmode := envOr("POSTGRES_SSLMODE", "disable")

	dsn := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s", host, port, user, dbname, sslmode)
	if password != "" {
		dsn += " password=" + password
	}
	return dsn
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitEnv(key, fallback string) []string {
	v := envOr(key, fallback)
	var result []string
	for _, s := range splitComma(v) {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}
