package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	googleOAuthAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleOAuthTokenURL = "https://oauth2.googleapis.com/token"
	googleScopeAdwords  = "https://www.googleapis.com/auth/adwords"
	googleScopeGBP      = "https://www.googleapis.com/auth/business.manage"
)

func googleRefreshAccessToken(httpClient *http.Client, clientID, clientSecret, refreshToken string) (string, time.Time, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	resp, err := httpClient.PostForm(googleOAuthTokenURL, form)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("google oauth: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("google oauth HTTP %d: %s", resp.StatusCode, recortar(string(body), 300))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", time.Time{}, err
	}
	if tok.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("google oauth: %s %s", tok.Error, tok.ErrorDesc)
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	return tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second), nil
}

func runGoogleOAuth() {
	_ = godotenv.Load()
	clientID := strings.TrimSpace(firstNonEmpty(os.Getenv("GMB_CLIENT_ID"), os.Getenv("GOOGLE_ADS_CLIENT_ID")))
	clientSecret := strings.TrimSpace(firstNonEmpty(os.Getenv("GMB_CLIENT_SECRET"), os.Getenv("GOOGLE_ADS_CLIENT_SECRET")))
	if clientID == "" || clientSecret == "" {
		log.Fatal("faltan GMB_CLIENT_ID/GMB_CLIENT_SECRET o GOOGLE_ADS_CLIENT_ID/GOOGLE_ADS_CLIENT_SECRET")
	}

	redirect := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_REDIRECT"))
	if redirect == "" {
		redirect = "http://127.0.0.1:8765/callback"
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" {
		log.Fatalf("GOOGLE_OAUTH_REDIRECT inválido: %s", redirect)
	}
	listenHost := u.Host

	ln, err := net.Listen("tcp", listenHost)
	if err != nil {
		log.Fatalf("no se pudo escuchar en %s: %v\nAñade esta URI exacta en el cliente OAuth (Authorized redirect URIs):\n  %s", listenHost, err, redirect)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		if e := r.URL.Query().Get("error"); e != "" {
			msg := e
			if d := r.URL.Query().Get("error_description"); d != "" {
				msg += ": " + d
			}
			_, _ = w.Write([]byte("Error OAuth: " + msg + "\nPuedes cerrar esta ventana."))
			errCh <- fmt.Errorf("%s", msg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "falta code", http.StatusBadRequest)
			errCh <- fmt.Errorf("callback sin code")
			return
		}
		_, _ = w.Write([]byte("Autorización OK. Ya puedes volver a la terminal y cerrar esta ventana."))
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	scopes := googleScopeAdwords + " " + googleScopeGBP
	authURL := googleOAuthAuthURL + "?" + url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {scopes},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}.Encode()

	fmt.Println("=== OAuth Google: Ads + Business Profile ===")
	fmt.Println()
	fmt.Println("Antes de abrir el enlace, en Google Cloud Console:")
	fmt.Println("  1) APIs habilitadas: Google Ads API, My Business Account Management API,")
	fmt.Println("     My Business Business Information API, Business Profile Performance API")
	fmt.Println("  2) En el cliente OAuth, Authorized redirect URIs debe incluir EXACTO:")
	fmt.Println("     " + redirect)
	fmt.Println("  3) Solicita acceso GBP (formulario de Google) si aún no está aprobado")
	fmt.Println()
	fmt.Println("Abre esta URL con la cuenta que administra Ads Y los perfiles de las oficinas:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("Esperando callback en", redirect, "...")

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		_ = srv.Shutdown(context.Background())
		log.Fatalf("oauth: %v", err)
	case <-time.After(10 * time.Minute):
		_ = srv.Shutdown(context.Background())
		log.Fatal("tiempo de espera agotado (10 min)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirect)

	resp, err := http.PostForm(googleOAuthTokenURL, form)
	if err != nil {
		log.Fatalf("intercambiar code: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Fatalf("token HTTP %d: %s", resp.StatusCode, recortar(string(body), 400))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		log.Fatal(err)
	}
	if tok.RefreshToken == "" {
		fmt.Println("Google no devolvió refresh_token. Suele pasar si ya autorizaste antes.")
		fmt.Println("Vuelve a correr con prompt=consent (este comando ya lo pide) o revoca el acceso en:")
		fmt.Println("  https://myaccount.google.com/permissions")
		fmt.Println("Respuesta:", recortar(string(body), 400))
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Scopes concedidos:", tok.Scope)
	fmt.Println()
	fmt.Println("Copia esto en .env (el mismo refresh token sirve para Ads y My Business):")
	fmt.Println()
	fmt.Println("GOOGLE_ADS_REFRESH_TOKEN=" + tok.RefreshToken)
	fmt.Println("GMB_REFRESH_TOKEN=" + tok.RefreshToken)
	fmt.Println()
	fmt.Println("Luego prueba:")
	fmt.Println("  go run . -probe-gmb")
	fmt.Println("  go run . -probe-google-ads")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
