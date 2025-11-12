package internal

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken string
	AdminSecret   string
	DatabaseURL   string
	Port          string
	UseWebhook    bool
	PublicBaseURL string
	WebhookPath   string
	APIToken      string
}

func LoadConfig() *Config {
	_ = godotenv.Load() // ignore error if .env is absent

	cfg := &Config{
		TelegramToken: os.Getenv("TELEGRAM_TOKEN"),
		AdminSecret:   os.Getenv("ADMIN_SECRET"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		Port:          getenvDefault("PORT", "8080"),
		PublicBaseURL: os.Getenv("PUBLIC_BASE_URL"),
		WebhookPath:   getenvDefault("WEBHOOK_PATH", "/webhook/telegram"),
		APIToken:      getenvDefault("API_TOKEN", os.Getenv("ADMIN_SECRET")),
	}

	if cfg.TelegramToken == "" || cfg.AdminSecret == "" || cfg.DatabaseURL == "" {
		log.Fatal("TELEGRAM_TOKEN, ADMIN_SECRET, DATABASE_URL must be set")
	}

	if v := os.Getenv("USE_WEBHOOK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			cfg.UseWebhook = b
		}
	}

	return cfg
}

func getenvDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
