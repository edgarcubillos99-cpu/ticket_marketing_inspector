package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	UbersmithBaseURL string
	UbersmithUser    string
	UbersmithToken   string
	UbersmithLimit   int

	OpenAIKey   string
	OpenAIModel string

	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string
	MySQLTable    string
	MySQLTLS      string

	Port       string
	Workers    int
	RunOnStart bool
	CronSpec   string
	CronTZ     string

	Backfill     bool
	BackfillFrom string
}

func LoadConfig() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		UbersmithBaseURL: envOr("UBERSMITH_BASE_URL", "https://billing.osnetpr.com/api/2.0/"),
		UbersmithUser:    strings.TrimSpace(os.Getenv("UBERSMITH_USER")),
		UbersmithToken:   strings.TrimSpace(os.Getenv("UBERSMITH_TOKEN")),
		UbersmithLimit:   envInt("UBERSMITH_LIMIT", 0),
		OpenAIKey:        strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:      envOr("OPENAI_MODEL", "gpt-5.6-luna"),
		MySQLHost:        strings.TrimSpace(os.Getenv("MYSQL_HOST")),
		MySQLPort:        envOr("MYSQL_PORT", "3306"),
		MySQLUser:        strings.TrimSpace(os.Getenv("MYSQL_USER")),
		MySQLPassword:    os.Getenv("MYSQL_PASSWORD"),
		MySQLDatabase:    strings.TrimSpace(os.Getenv("MYSQL_DATABASE")),
		MySQLTable:       envOr("MYSQL_TABLE", "tickets_osnet"),
		MySQLTLS:         strings.TrimSpace(os.Getenv("MYSQL_TLS")),
		Port:             envOr("PORT", "8080"),
		Workers:          envInt("WORKERS", 3),
		RunOnStart:       envBool("RUN_ON_START"),
		CronSpec:         envOr("CRON_SPEC", "0 1 * * 0"),
		CronTZ:           envOr("CRON_TZ", "America/Puerto_Rico"),
		Backfill:         envBool("BACKFILL"),
		BackfillFrom:     envOr("BACKFILL_FROM", "2024-01-01"),
	}

	if cfg.Workers < 1 {
		cfg.Workers = 1
	}

	var missing []string
	if cfg.UbersmithUser == "" {
		missing = append(missing, "UBERSMITH_USER")
	}
	if cfg.UbersmithToken == "" {
		missing = append(missing, "UBERSMITH_TOKEN")
	}
	if cfg.OpenAIKey == "" {
		missing = append(missing, "OPENAI_API_KEY")
	}
	if cfg.MySQLHost == "" {
		missing = append(missing, "MYSQL_HOST")
	}
	if cfg.MySQLUser == "" {
		missing = append(missing, "MYSQL_USER")
	}
	if cfg.MySQLDatabase == "" {
		missing = append(missing, "MYSQL_DATABASE")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("faltan variables en .env: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
