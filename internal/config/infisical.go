package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultInfisicalPath = "/api"
	defaultInfisicalSite = "https://app.infisical.com"
	infisicalLoginPath   = "/api/v1/auth/universal-auth/login"
	infisicalSecretsPath = "/api/v3/secrets/raw"
	infisicalHTTPTimeout = 30 * time.Second
)

// LoadEnv carga .env (si existe) y, si hay identidad de máquina, inyecta
// secretos de Infisical. Las variables ya definidas en el proceso o en .env
// no se sobrescriben, salvo INFISICAL_OVERWRITE=true.
func LoadEnv() error {
	_ = godotenv.Load()
	return loadInfisical()
}

func loadInfisical() error {
	clientID := firstNonEmpty(
		os.Getenv("INFISICAL_CLIENT_ID"),
		os.Getenv("INFISICAL_UNIVERSAL_AUTH_CLIENT_ID"),
	)
	clientSecret := firstNonEmpty(
		os.Getenv("INFISICAL_CLIENT_SECRET"),
		os.Getenv("INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET"),
	)
	projectID := strings.TrimSpace(os.Getenv("INFISICAL_PROJECT_ID"))
	envSlug := firstNonEmpty(os.Getenv("INFISICAL_ENV"), os.Getenv("INFISICAL_ENVIRONMENT"), "dev")
	secretPath := firstNonEmpty(os.Getenv("INFISICAL_PATH"), os.Getenv("INFISICAL_SECRET_PATH"), defaultInfisicalPath)
	siteURL := strings.TrimRight(firstNonEmpty(os.Getenv("INFISICAL_DOMAIN"), defaultInfisicalSite), "/")
	overwrite := envBool("INFISICAL_OVERWRITE")

	if err := validateInfisicalCreds(clientID, clientSecret, projectID); err != nil {
		return err
	}
	if clientID == "" {
		return nil
	}

	httpClient := &http.Client{Timeout: infisicalHTTPTimeout}
	token, err := infisicalLogin(httpClient, siteURL, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("infisical login: %w", err)
	}
	secrets, err := infisicalListSecrets(httpClient, siteURL, token, projectID, envSlug, secretPath)
	if err != nil {
		return fmt.Errorf("infisical listar secretos (env=%s path=%s): %w", envSlug, secretPath, err)
	}

	injected, skipped := applyInfisicalSecrets(secrets, overwrite)
	log.Printf("infisical: env=%s path=%s inyectados=%d omitidos=%d", envSlug, secretPath, injected, skipped)
	return nil
}

func validateInfisicalCreds(clientID, clientSecret, projectID string) error {
	if clientID == "" && clientSecret == "" {
		return nil
	}
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("INFISICAL_CLIENT_ID e INFISICAL_CLIENT_SECRET deben definirse juntos")
	}
	if projectID == "" {
		return fmt.Errorf("INFISICAL_PROJECT_ID es obligatorio cuando Infisical está habilitado")
	}
	return nil
}

type infisicalSecret struct {
	SecretKey   string
	SecretValue string
}

func applyInfisicalSecrets(secrets []infisicalSecret, overwrite bool) (injected, skipped int) {
	for _, s := range secrets {
		key := strings.TrimSpace(s.SecretKey)
		if key == "" || strings.HasPrefix(key, "INFISICAL_") {
			skipped++
			continue
		}
		if !overwrite && os.Getenv(key) != "" {
			skipped++
			continue
		}
		_ = os.Setenv(key, s.SecretValue)
		injected++
	}
	return injected, skipped
}

func infisicalLogin(httpClient *http.Client, siteURL, clientID, clientSecret string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, siteURL+infisicalLoginPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, recortar(string(raw), 300))
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("JSON: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("la respuesta no incluyó accessToken")
	}
	return out.AccessToken, nil
}

func infisicalListSecrets(httpClient *http.Client, siteURL, token, projectID, envSlug, secretPath string) ([]infisicalSecret, error) {
	q := url.Values{}
	q.Set("workspaceId", projectID)
	q.Set("environment", envSlug)
	q.Set("secretPath", secretPath)
	q.Set("include_imports", "true")
	q.Set("expandSecretReferences", "true")

	req, err := http.NewRequest(http.MethodGet, siteURL+infisicalSecretsPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, recortar(string(raw), 300))
	}

	var out struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
		} `json:"secrets"`
		Imports []struct {
			Secrets []struct {
				SecretKey   string `json:"secretKey"`
				SecretValue string `json:"secretValue"`
			} `json:"secrets"`
		} `json:"imports"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("JSON: %w", err)
	}

	seen := map[string]struct{}{}
	secrets := make([]infisicalSecret, 0, len(out.Secrets))
	add := func(key, value string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		secrets = append(secrets, infisicalSecret{SecretKey: key, SecretValue: value})
	}
	for _, s := range out.Secrets {
		add(s.SecretKey, s.SecretValue)
	}
	for _, imp := range out.Imports {
		for _, s := range imp.Secrets {
			add(s.SecretKey, s.SecretValue)
		}
	}
	return secrets, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
