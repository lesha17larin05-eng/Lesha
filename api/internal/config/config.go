package config

import (
	"os"
	"time"
)

type Config struct {
	AppEnv             string
	AppHost            string
	JWTSecret          string
	SessionSecret      string
	DatabaseURL        string
	SMTPHost           string
	SMTPPort           string
	SMTPUser           string
	SMTPPassword       string
	SMTPFrom           string
	ProdamusPayformURL string
	ProdamusSecret     string
	ProdamusTestMode   bool
	VideoTokenSecret   string
	VideoTokenTTL      time.Duration
	VideoStoragePath   string
	AdminEmail         string
	AdminPassword      string
	UserEmail          string
	UserPassword       string
	CORSOrigin         string
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func Load() *Config {
	ttl, err := time.ParseDuration(env("VIDEO_TOKEN_TTL", "2h"))
	if err != nil {
		ttl = 2 * time.Hour
	}
	return &Config{
		AppEnv:             env("APP_ENV", "development"),
		AppHost:            env("APP_HOST", "http://localhost"),
		JWTSecret:          env("JWT_SECRET", "dev-jwt-secret"),
		SessionSecret:      env("SESSION_SECRET", "dev-session-secret"),
		DatabaseURL:        env("DATABASE_URL", "postgres://app:app@postgres:5432/app?sslmode=disable"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           env("SMTP_PORT", "465"),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:           env("SMTP_FROM", "noreply@leshalarin.ru"),
		ProdamusPayformURL: env("PRODAMUS_PAYFORM_URL", "https://leshalarin.payform.ru"),
		ProdamusSecret:     env("PRODAMUS_SECRET_KEY", "test"),
		ProdamusTestMode:   env("PRODAMUS_TEST_MODE", "true") == "true",
		VideoTokenSecret:   env("VIDEO_TOKEN_SECRET", "dev-video-secret"),
		VideoTokenTTL:      ttl,
		VideoStoragePath:   env("VIDEO_STORAGE_PATH", "/var/videos"),
		AdminEmail:         env("ADMIN_EMAIL", "admin@leshalarin.ru"),
		AdminPassword:      env("ADMIN_PASSWORD", "admin12345"),
		UserEmail:          env("USER_EMAIL", "user@leshalarin.ru"),
		UserPassword:       env("USER_PASSWORD", "user12345"),
		CORSOrigin:         env("CORS_ORIGIN", "http://localhost"),
	}
}
