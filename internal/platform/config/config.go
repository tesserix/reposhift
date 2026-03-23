package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	ModeSaaS       = "saas"
	ModeSelfHosted = "selfhosted"
)

type PlatformConfig struct {
	Mode           string
	Port           string
	DatabaseURL    string
	EncryptionKey  string // 32-byte hex key for AES-256-GCM (SaaS mode)
	JWTSecret      string
	AdminToken     string // Self-hosted: static bearer token for admin access
	K8sNamespace   string
	OperatorAPIURL string
	GitHub         GitHubOAuthConfig
	CORS           CORSConfig
}

type GitHubOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type CORSConfig struct {
	AllowedOrigins []string
}

func Load() (*PlatformConfig, error) {
	cfg := &PlatformConfig{
		Mode:           envOr("REPOSHIFT_MODE", ModeSaaS),
		Port:           envOr("PORT", "8090"),
		DatabaseURL:    buildDatabaseURL(),
		EncryptionKey:  os.Getenv("ENCRYPTION_KEY"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		K8sNamespace:   envOr("K8S_NAMESPACE", "marketplace"),
		OperatorAPIURL: envOr("OPERATOR_API_URL", "http://reposhift-operator:8080"),
		GitHub: GitHubOAuthConfig{
			ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
			ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("GITHUB_REDIRECT_URL"),
			Scopes:       []string{"user:email", "read:org"},
		},
		CORS: CORSConfig{
			AllowedOrigins: splitEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3005"),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *PlatformConfig) IsSaaS() bool {
	return c.Mode == ModeSaaS
}

func (c *PlatformConfig) IsSelfHosted() bool {
	return c.Mode == ModeSelfHosted
}

// GitHubOAuthEnabled returns true when GitHub OAuth credentials are configured.
func (c *PlatformConfig) GitHubOAuthEnabled() bool {
	return c.GitHub.ClientID != "" && c.GitHub.ClientSecret != ""
}

func (c *PlatformConfig) Validate() error {
	if c.Mode != ModeSaaS && c.Mode != ModeSelfHosted {
		return fmt.Errorf("REPOSHIFT_MODE must be 'saas' or 'selfhosted', got %q", c.Mode)
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	// JWT_SECRET is required unless running in self-hosted mode with admin token
	// only (no GitHub OAuth), since JWTs are never issued in that configuration.
	needsJWT := c.IsSaaS() || c.GitHub.ClientID != ""
	if needsJWT {
		if c.JWTSecret == "" {
			return fmt.Errorf("JWT_SECRET is required when GitHub OAuth is configured or in SaaS mode")
		}
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters long")
		}
	}
	if c.IsSaaS() {
		if c.EncryptionKey == "" {
			return fmt.Errorf("ENCRYPTION_KEY is required in SaaS mode")
		}
		if len(c.EncryptionKey) != 64 {
			return fmt.Errorf("ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
		}
		if c.GitHub.ClientID == "" || c.GitHub.ClientSecret == "" {
			return fmt.Errorf("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET are required in SaaS mode")
		}
		if c.GitHub.RedirectURL == "" {
			return fmt.Errorf("GITHUB_REDIRECT_URL is required in SaaS mode")
		}
	}
	if c.IsSelfHosted() && c.AdminToken == "" && c.GitHub.ClientID == "" {
		return fmt.Errorf("self-hosted mode requires either ADMIN_TOKEN or GitHub OAuth config")
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
