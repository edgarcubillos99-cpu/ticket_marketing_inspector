package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type GoogleAdsClient struct {
	developerToken  string
	clientID        string
	clientSecret    string
	refreshToken    string
	customerID      string
	loginCustomerID string
	apiVersion      string
	http            *http.Client
	class           *clasificadorCliente

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGoogleAdsClient(cfg *Config) *GoogleAdsClient {
	return &GoogleAdsClient{
		developerToken:  cfg.GoogleAdsDeveloperToken,
		clientID:        cfg.GoogleAdsClientID,
		clientSecret:    cfg.GoogleAdsClientSecret,
		refreshToken:    cfg.GoogleAdsRefreshToken,
		customerID:      cfg.GoogleAdsCustomerID,
		loginCustomerID: cfg.GoogleAdsLoginCustomerID,
		apiVersion:      strings.TrimPrefix(cfg.GoogleAdsAPIVersion, "v"),
		http:            &http.Client{Timeout: 120 * time.Second},
		class:           newClasificadorCliente(cfg.AdsResidencialPattern, cfg.AdsComercialPattern),
	}
}

func (c *GoogleAdsClient) ensureToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("refresh_token", c.refreshToken)
	form.Set("grant_type", "refresh_token")

	resp, err := c.http.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		return "", fmt.Errorf("google oauth: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("google oauth HTTP %d: %s", resp.StatusCode, recortar(string(body), 300))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("google oauth: %s %s", tok.Error, tok.ErrorDesc)
	}
	c.accessToken = tok.AccessToken
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

// FetchAds consulta métricas mensuales agregadas por campaña (clics + costo).
func (c *GoogleAdsClient) FetchAds(desde, hasta time.Time, loc *time.Location) ([]MetricaAnuncio, error) {
	inicio := primerDiaMes(desde, loc)
	finIncl := primerDiaMes(hasta, loc).AddDate(0, 1, 0).AddDate(0, 0, -1)

	query := fmt.Sprintf(`
SELECT
  campaign.name,
  segments.month,
  metrics.clicks,
  metrics.cost_micros
FROM campaign
WHERE segments.date BETWEEN '%s' AND '%s'
  AND campaign.status != 'REMOVED'
`, inicio.Format("2006-01-02"), finIncl.Format("2006-01-02"))

	rows, err := c.search(query)
	if err != nil {
		return nil, err
	}

	agg := map[string]*MetricaAnuncio{}
	for _, raw := range rows {
		var row struct {
			Campaign struct {
				Name string `json:"name"`
			} `json:"campaign"`
			Segments struct {
				Month string `json:"month"`
			} `json:"segments"`
			Metrics struct {
				Clicks     json.RawMessage `json:"clicks"`
				CostMicros json.RawMessage `json:"costMicros"`
			} `json:"metrics"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			continue
		}

		tipoCliente := c.class.ClasificarTipoCliente(row.Campaign.Name)
		if tipoCliente == "" {
			continue
		}

		mesStr := strings.TrimSpace(row.Segments.Month)
		// segments.month viene como YYYY-MM-01 o YYYY-MM
		if len(mesStr) == 7 {
			mesStr += "-01"
		}
		mes, err := time.ParseInLocation("2006-01-02", mesStr, loc)
		if err != nil {
			continue
		}
		mes = primerDiaMes(mes, loc)

		clicks := parseJSONInt(row.Metrics.Clicks)
		costMicros := parseJSONInt(row.Metrics.CostMicros)
		inversion := float64(costMicros) / 1_000_000.0

		key := PlataformaGoogle + "|" + tipoCliente + "|" + mes.Format("2006-01") + "|" + TipoResultadoClics
		if cur, ok := agg[key]; ok {
			cur.Resultado += clicks
			cur.Inversion += inversion
		} else {
			agg[key] = &MetricaAnuncio{
				Plataforma:    PlataformaGoogle,
				TipoCliente:   tipoCliente,
				Mes:           mes,
				TipoResultado: TipoResultadoClics,
				Resultado:     clicks,
				Inversion:     inversion,
			}
		}
	}

	out := make([]MetricaAnuncio, 0, len(agg))
	for _, v := range agg {
		// Redondeo a 2 decimales
		v.Inversion = float64(int64(v.Inversion*100+0.5)) / 100
		out = append(out, *v)
	}
	return out, nil
}

func (c *GoogleAdsClient) search(query string) ([]json.RawMessage, error) {
	token, err := c.ensureToken()
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf(
		"https://googleads.googleapis.com/v%s/customers/%s/googleAds:searchStream",
		c.apiVersion, c.customerID,
	)

	payload, _ := json.Marshal(map[string]string{"query": strings.TrimSpace(query)})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("developer-token", c.developerToken)
	req.Header.Set("Content-Type", "application/json")
	if c.loginCustomerID != "" {
		req.Header.Set("login-customer-id", c.loginCustomerID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google ads HTTP %d: %s", resp.StatusCode, recortar(string(body), 500))
	}

	// searchStream puede devolver un array de batches o un solo objeto.
	var batches []struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &batches); err == nil && len(batches) > 0 {
		var all []json.RawMessage
		for _, b := range batches {
			all = append(all, b.Results...)
		}
		return all, nil
	}

	var single struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, fmt.Errorf("google ads JSON: %w (%s)", err, recortar(string(body), 300))
	}
	return single.Results, nil
}
