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

	// Tablas de marketing (redes + anuncios + GBP + emails)
	MySQLTableSocial     string
	MySQLTableAds        string
	MySQLTableMyBusiness string
	MySQLTableEmails     string

	Port       string
	Workers    int
	RunOnStart bool
	CronSpec   string
	CronTZ     string

	Backfill     bool
	BackfillFrom string

	// Cuántos meses hacia atrás sincronizar en cada corrida semanal de marketing.
	MarketingMonthsLookback int

	// Meta (Facebook + Instagram orgánico y ads)
	MetaAccessToken             string
	MetaAPIVersion              string
	FacebookPageID              string
	InstagramBusinessAccountID  string
	MetaAdAccountID             string // sin prefijo act_

	// LinkedIn (orgánico + ads)
	LinkedInAccessToken  string
	LinkedInOrganizationID string
	LinkedInAdAccountID  string
	LinkedInAPIVersion   string

	// Google Ads
	GoogleAdsDeveloperToken  string
	GoogleAdsClientID        string
	GoogleAdsClientSecret    string
	GoogleAdsRefreshToken    string
	GoogleAdsCustomerID      string // sin guiones
	GoogleAdsLoginCustomerID string // MCC opcional, sin guiones
	GoogleAdsAPIVersion      string

	// Clasificación Residencial / Comercial por nombre de campaña (regex case-insensitive)
	AdsResidencialPattern string
	AdsComercialPattern   string

	// Google Business Profile (My Business)
	GMBClientID     string
	GMBClientSecret string
	GMBRefreshToken string
	GMBAccountID    string // opcional: accounts/123 o solo el número
	GMBLocationMap  string // Humacao:locations/111;Yauco:locations/222

	// Emails / formularios del plugin WordPress
	EmailsProvider      string // gravityforms (vacío = omitir)
	EmailsWPBaseURL     string
	EmailsWPUser        string
	EmailsWPAppPassword string
	EmailsFormMap       string // 1:Cobertura:Residencial;2:Internet Residencial:Residencial
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
		MySQLTableSocial:     envOr("MYSQL_TABLE_SOCIAL", "redes_sociales_metricas"),
		MySQLTableAds:        envOr("MYSQL_TABLE_ADS", "anuncios_metricas"),
		MySQLTableMyBusiness: envOr("MYSQL_TABLE_MYBUSINESS", "mybusiness_metricas"),
		MySQLTableEmails:     envOr("MYSQL_TABLE_EMAILS", "emails_metricas"),
		Port:             envOr("PORT", "8080"),
		Workers:          envInt("WORKERS", 3),
		RunOnStart:       envBool("RUN_ON_START"),
		CronSpec:         envOr("CRON_SPEC", "0 1 * * 0"),
		CronTZ:           envOr("CRON_TZ", "America/Puerto_Rico"),
		Backfill:         envBool("BACKFILL"),
		BackfillFrom:     envOr("BACKFILL_FROM", "2024-01-01"),

		MarketingMonthsLookback: envInt("MARKETING_MONTHS_LOOKBACK", 3),

		MetaAccessToken:            strings.TrimSpace(os.Getenv("META_ACCESS_TOKEN")),
		MetaAPIVersion:             envOr("META_API_VERSION", "v26.0"),
		FacebookPageID:             strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ID")),
		InstagramBusinessAccountID: strings.TrimSpace(os.Getenv("INSTAGRAM_BUSINESS_ACCOUNT_ID")),
		MetaAdAccountID:            strings.TrimLeft(strings.TrimSpace(os.Getenv("META_AD_ACCOUNT_ID")), "act_"),

		LinkedInAccessToken:    strings.TrimSpace(os.Getenv("LINKEDIN_ACCESS_TOKEN")),
		LinkedInOrganizationID: strings.TrimSpace(os.Getenv("LINKEDIN_ORGANIZATION_ID")),
		LinkedInAdAccountID:    strings.TrimSpace(os.Getenv("LINKEDIN_AD_ACCOUNT_ID")),
		LinkedInAPIVersion:     envOr("LINKEDIN_API_VERSION", "202411"),

		GoogleAdsDeveloperToken:  strings.TrimSpace(os.Getenv("GOOGLE_ADS_DEVELOPER_TOKEN")),
		GoogleAdsClientID:        strings.TrimSpace(os.Getenv("GOOGLE_ADS_CLIENT_ID")),
		GoogleAdsClientSecret:    strings.TrimSpace(os.Getenv("GOOGLE_ADS_CLIENT_SECRET")),
		GoogleAdsRefreshToken:    strings.TrimSpace(os.Getenv("GOOGLE_ADS_REFRESH_TOKEN")),
		GoogleAdsCustomerID:      strings.ReplaceAll(strings.TrimSpace(os.Getenv("GOOGLE_ADS_CUSTOMER_ID")), "-", ""),
		GoogleAdsLoginCustomerID: strings.ReplaceAll(strings.TrimSpace(os.Getenv("GOOGLE_ADS_LOGIN_CUSTOMER_ID")), "-", ""),
		GoogleAdsAPIVersion:      envOr("GOOGLE_ADS_API_VERSION", "v25"),

		AdsResidencialPattern: envOr("ADS_RESIDENCIAL_PATTERN", `(?i)residenc|residential|\bres\b|hogar|instalaci|router|back\s*to\s*school|bts|b[uú]squeda|clientes potenciales|leads|acp|promo|v[ií]deo|display|anuncio`),
		AdsComercialPattern:   envOr("ADS_COMERCIAL_PATTERN", `(?i)comercial|commercial|business|\bb2b\b|small\s*business`),

		GMBClientID:     strings.TrimSpace(os.Getenv("GMB_CLIENT_ID")),
		GMBClientSecret: strings.TrimSpace(os.Getenv("GMB_CLIENT_SECRET")),
		GMBRefreshToken: strings.TrimSpace(os.Getenv("GMB_REFRESH_TOKEN")),
		GMBAccountID:    strings.TrimSpace(os.Getenv("GMB_ACCOUNT_ID")),
		GMBLocationMap:  strings.TrimSpace(os.Getenv("GMB_LOCATION_MAP")),

		EmailsProvider:      strings.ToLower(strings.TrimSpace(os.Getenv("EMAILS_PROVIDER"))),
		EmailsWPBaseURL:     strings.TrimRight(strings.TrimSpace(os.Getenv("EMAILS_WP_BASE_URL")), "/"),
		EmailsWPUser:        strings.TrimSpace(os.Getenv("EMAILS_WP_USER")),
		EmailsWPAppPassword: strings.TrimSpace(os.Getenv("EMAILS_WP_APP_PASSWORD")),
		EmailsFormMap:       strings.TrimSpace(os.Getenv("EMAILS_FORM_MAP")),
	}

	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.MarketingMonthsLookback < 1 {
		cfg.MarketingMonthsLookback = 1
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

func (c *Config) MetaOrganicoEnabled() bool {
	return c.MetaAccessToken != "" && (c.FacebookPageID != "" || c.InstagramBusinessAccountID != "")
}

func (c *Config) MetaAdsEnabled() bool {
	return c.MetaAccessToken != "" && c.MetaAdAccountID != ""
}

func (c *Config) LinkedInOrganicoEnabled() bool {
	return c.LinkedInAccessToken != "" && c.LinkedInOrganizationID != ""
}

func (c *Config) LinkedInAdsEnabled() bool {
	return c.LinkedInAccessToken != "" && c.LinkedInAdAccountID != ""
}

func (c *Config) GoogleAdsEnabled() bool {
	return c.GoogleAdsDeveloperToken != "" &&
		c.GoogleAdsClientID != "" &&
		c.GoogleAdsClientSecret != "" &&
		c.GoogleAdsRefreshToken != "" &&
		c.GoogleAdsCustomerID != ""
}

func (c *Config) gmbOAuthClientID() string {
	if c.GMBClientID != "" {
		return c.GMBClientID
	}
	return c.GoogleAdsClientID
}

func (c *Config) gmbOAuthClientSecret() string {
	if c.GMBClientSecret != "" {
		return c.GMBClientSecret
	}
	return c.GoogleAdsClientSecret
}

func (c *Config) gmbOAuthRefreshToken() string {
	if c.GMBRefreshToken != "" {
		return c.GMBRefreshToken
	}
	return c.GoogleAdsRefreshToken
}

func (c *Config) GMBEnabled() bool {
	return c.gmbOAuthClientID() != "" &&
		c.gmbOAuthClientSecret() != "" &&
		c.GMBRefreshToken != ""
}

func (c *Config) EmailsEnabled() bool {
	if c.EmailsProvider != "gravityforms" {
		return false
	}
	return c.EmailsWPBaseURL != "" && c.EmailsWPUser != "" &&
		c.EmailsWPAppPassword != "" && c.EmailsFormMap != ""
}

func (c *Config) MarketingEnabled() bool {
	return c.MetaOrganicoEnabled() || c.MetaAdsEnabled() ||
		c.LinkedInOrganicoEnabled() || c.LinkedInAdsEnabled() ||
		c.GoogleAdsEnabled() || c.GMBEnabled() || c.EmailsEnabled()
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
