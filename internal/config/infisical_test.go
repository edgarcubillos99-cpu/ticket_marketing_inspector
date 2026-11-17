package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestValidateInfisicalCreds(t *testing.T) {
	t.Parallel()
	if err := validateInfisicalCreds("", "", ""); err != nil {
		t.Fatalf("sin credenciales debe omitirse: %v", err)
	}
	if err := validateInfisicalCreds("id", "", "proj"); err == nil {
		t.Fatal("client id sin secret debe fallar")
	}
	if err := validateInfisicalCreds("id", "secret", ""); err == nil {
		t.Fatal("sin project id debe fallar")
	}
	if err := validateInfisicalCreds("id", "secret", "proj"); err != nil {
		t.Fatalf("creds completas: %v", err)
	}
}

func TestApplyInfisicalSecretsNoOverwrite(t *testing.T) {
	t.Setenv("YA_EXISTE", "local")
	t.Setenv("VACIA", "")

	secrets := []infisicalSecret{
		{SecretKey: "YA_EXISTE", SecretValue: "infisical"},
		{SecretKey: "NUEVA", SecretValue: "desde-infisical"},
		{SecretKey: "INFISICAL_CLIENT_SECRET", SecretValue: "no-tocar"},
		{SecretKey: "VACIA", SecretValue: "rellenar"},
	}
	injected, skipped := applyInfisicalSecrets(secrets, false)
	if injected != 2 {
		t.Fatalf("inyectados=%d, quiero 2", injected)
	}
	if skipped != 2 {
		t.Fatalf("omitidos=%d, quiero 2", skipped)
	}
	if os.Getenv("YA_EXISTE") != "local" {
		t.Fatalf("no debía sobrescribir YA_EXISTE: %s", os.Getenv("YA_EXISTE"))
	}
	if os.Getenv("NUEVA") != "desde-infisical" {
		t.Fatalf("NUEVA=%s", os.Getenv("NUEVA"))
	}
	if os.Getenv("VACIA") != "rellenar" {
		t.Fatalf("VACIA=%s", os.Getenv("VACIA"))
	}
}

func TestApplyInfisicalSecretsOverwrite(t *testing.T) {
	t.Setenv("YA_EXISTE", "local")
	secrets := []infisicalSecret{
		{SecretKey: "YA_EXISTE", SecretValue: "infisical"},
	}
	injected, skipped := applyInfisicalSecrets(secrets, true)
	if injected != 1 || skipped != 0 {
		t.Fatalf("inyectados=%d omitidos=%d", injected, skipped)
	}
	if os.Getenv("YA_EXISTE") != "infisical" {
		t.Fatalf("YA_EXISTE=%s", os.Getenv("YA_EXISTE"))
	}
}

func TestInfisicalLoginAndList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case infisicalLoginPath:
			_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "tok-123"})
		case infisicalSecretsPath:
			if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
				t.Errorf("Authorization=%s", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"secrets": []map[string]string{
					{"secretKey": "OPENAI_API_KEY", "secretValue": "sk-test"},
				},
				"imports": []map[string]any{
					{"secrets": []map[string]string{
						{"secretKey": "MYSQL_HOST", "secretValue": "db.interno"},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	token, err := infisicalLogin(srv.Client(), srv.URL, "id", "secret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token != "tok-123" {
		t.Fatalf("token=%s", token)
	}
	secrets, err := infisicalListSecrets(srv.Client(), srv.URL, token, "proj", "dev", "/marketing")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("secretos=%d %+v", len(secrets), secrets)
	}
}
