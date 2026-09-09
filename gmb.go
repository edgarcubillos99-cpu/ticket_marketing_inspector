package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GMBLocation struct {
	Sede     string
	Location string // locations/{id}
	Title    string
	Account  string
}

type GMBClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	accountID    string
	locationMap  map[string]string // sede -> locations/id
	http         *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewGMBClient(cfg *Config) *GMBClient {
	return &GMBClient{
		clientID:     cfg.gmbOAuthClientID(),
		clientSecret: cfg.gmbOAuthClientSecret(),
		refreshToken: cfg.gmbOAuthRefreshToken(),
		accountID:    normalizeGMBAccountID(cfg.GMBAccountID),
		locationMap:  parseGMBLocationMap(cfg.GMBLocationMap),
		http:         &http.Client{Timeout: 120 * time.Second},
	}
}

func normalizeGMBAccountID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(id, "accounts/")
	return "accounts/" + id
}

func parseGMBLocationMap(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sede, loc, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		sede = strings.TrimSpace(sede)
		loc = locationResourceName(strings.TrimSpace(loc))
		if sede == "" || loc == "locations/" {
			continue
		}
		out[sede] = loc
	}
	return out
}

func locationResourceName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, "locations/"); i >= 0 {
		return name[i:]
	}
	return "locations/" + strings.TrimPrefix(name, "/")
}

func (c *GMBClient) ensureToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}
	tok, exp, err := googleRefreshAccessToken(c.http, c.clientID, c.clientSecret, c.refreshToken)
	if err != nil {
		return "", err
	}
	c.accessToken = tok
	c.tokenExpiry = exp
	return c.accessToken, nil
}

func (c *GMBClient) get(rawURL string, params url.Values, out any) error {
	token, err := c.ensureToken()
	if err != nil {
		return err
	}
	if params != nil && len(params) > 0 {
		if strings.Contains(rawURL, "?") {
			rawURL += "&" + params.Encode()
		} else {
			rawURL += "?" + params.Encode()
		}
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gmb leer: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gmb HTTP %d: %s", resp.StatusCode, recortar(string(body), 500))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("gmb JSON: %w (%s)", err, recortar(string(body), 300))
	}
	return nil
}

func (c *GMBClient) ListAccounts() ([]gmbAccount, error) {
	var all []gmbAccount
	page := ""
	for {
		params := url.Values{}
		params.Set("pageSize", "20")
		if page != "" {
			params.Set("pageToken", page)
		}
		var resp struct {
			Accounts      []gmbAccount `json:"accounts"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get("https://mybusinessaccountmanagement.googleapis.com/v1/accounts", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Accounts...)
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}
	return all, nil
}

type gmbAccount struct {
	Name        string `json:"name"`
	AccountName string `json:"accountName"`
	Type        string `json:"type"`
}

type gmbLocationRaw struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	StoreCode string `json:"storeCode"`
}

func (c *GMBClient) ListLocations(accountName string) ([]gmbLocationRaw, error) {
	parent := strings.TrimSpace(accountName)
	if !strings.HasPrefix(parent, "accounts/") {
		parent = "accounts/" + strings.TrimPrefix(parent, "/")
	}
	var all []gmbLocationRaw
	page := ""
	for {
		params := url.Values{}
		params.Set("readMask", "name,title,storeCode")
		params.Set("pageSize", "100")
		if page != "" {
			params.Set("pageToken", page)
		}
		endpoint := "https://mybusinessbusinessinformation.googleapis.com/v1/" + parent + "/locations"
		var resp struct {
			Locations     []gmbLocationRaw `json:"locations"`
			NextPageToken string           `json:"nextPageToken"`
		}
		if err := c.get(endpoint, params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Locations...)
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}
	return all, nil
}

func (c *GMBClient) ResolveLocations() ([]GMBLocation, error) {
	if len(c.locationMap) > 0 {
		out := make([]GMBLocation, 0, len(c.locationMap))
		for sede, loc := range c.locationMap {
			out = append(out, GMBLocation{Sede: sede, Location: loc, Title: sede})
		}
		return out, nil
	}

	accounts, err := c.ListAccounts()
	if err != nil {
		return nil, fmt.Errorf("listar cuentas GBP: %w", err)
	}
	if c.accountID != "" {
		filtered := accounts[:0]
		for _, a := range accounts {
			if a.Name == c.accountID {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			accounts = []gmbAccount{{Name: c.accountID}}
		} else {
			accounts = filtered
		}
	}

	var out []GMBLocation
	seen := map[string]bool{}
	for _, acc := range accounts {
		locs, err := c.ListLocations(acc.Name)
		if err != nil {
			return nil, fmt.Errorf("listar sedes %s: %w", acc.Name, err)
		}
		for _, loc := range locs {
			res := locationResourceName(loc.Name)
			if seen[res] {
				continue
			}
			seen[res] = true
			sede := strings.TrimSpace(loc.Title)
			if sede == "" {
				sede = res
			}
			out = append(out, GMBLocation{
				Sede:     sede,
				Location: res,
				Title:    loc.Title,
				Account:  acc.Name,
			})
		}
	}
	return out, nil
}

func (c *GMBClient) Fetch(desde, hasta time.Time, loc *time.Location) ([]MetricaMyBusiness, error) {
	sedes, err := c.ResolveLocations()
	if err != nil {
		return nil, err
	}
	if len(sedes) == 0 {
		return nil, fmt.Errorf("gmb: no hay ubicaciones (configura GMB_LOCATION_MAP o verifica acceso a los perfiles)")
	}

	meses := mesesEnRango(desde, hasta, loc)
	var out []MetricaMyBusiness
	for _, sede := range sedes {
		for _, mes := range meses {
			m, err := c.fetchMes(sede, mes, loc)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", sede.Sede, mes.Format("2006-01"), err)
			}
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *GMBClient) fetchMes(sede GMBLocation, mes time.Time, loc *time.Location) (MetricaMyBusiness, error) {
	inicio := primerDiaMes(mes, loc)
	fin := inicio.AddDate(0, 1, 0).AddDate(0, 0, -1)
	hoy := time.Now().In(loc)
	if fin.After(hoy) {
		fin = hoy
	}

	params := url.Values{}
	for _, metric := range []string{
		"BUSINESS_IMPRESSIONS_DESKTOP_MAPS",
		"BUSINESS_IMPRESSIONS_DESKTOP_SEARCH",
		"BUSINESS_IMPRESSIONS_MOBILE_MAPS",
		"BUSINESS_IMPRESSIONS_MOBILE_SEARCH",
		"BUSINESS_CONVERSATIONS",
		"BUSINESS_DIRECTION_REQUESTS",
		"CALL_CLICKS",
		"WEBSITE_CLICKS",
	} {
		params.Add("dailyMetrics", metric)
	}
	params.Set("dailyRange.start_date.year", strconv.Itoa(inicio.Year()))
	params.Set("dailyRange.start_date.month", strconv.Itoa(int(inicio.Month())))
	params.Set("dailyRange.start_date.day", strconv.Itoa(inicio.Day()))
	params.Set("dailyRange.end_date.year", strconv.Itoa(fin.Year()))
	params.Set("dailyRange.end_date.month", strconv.Itoa(int(fin.Month())))
	params.Set("dailyRange.end_date.day", strconv.Itoa(fin.Day()))

	endpoint := fmt.Sprintf(
		"https://businessprofileperformance.googleapis.com/v1/%s:fetchMultiDailyMetricsTimeSeries",
		sede.Location,
	)

	var resp struct {
		MultiDailyMetricTimeSeries []struct {
			DailyMetricTimeSeries []struct {
				DailyMetric string `json:"dailyMetric"`
				TimeSeries  struct {
					DatedValues []struct {
						Date struct {
							Year  int `json:"year"`
							Month int `json:"month"`
							Day   int `json:"day"`
						} `json:"date"`
						Value string `json:"value"`
					} `json:"datedValues"`
				} `json:"timeSeries"`
			} `json:"dailyMetricTimeSeries"`
		} `json:"multiDailyMetricTimeSeries"`
	}
	if err := c.get(endpoint, params, &resp); err != nil {
		// Algunas cuentas no exponen conversaciones; reintenta sin ese métrico.
		params.Del("dailyMetrics")
		for _, metric := range []string{
			"BUSINESS_IMPRESSIONS_DESKTOP_MAPS",
			"BUSINESS_IMPRESSIONS_DESKTOP_SEARCH",
			"BUSINESS_IMPRESSIONS_MOBILE_MAPS",
			"BUSINESS_IMPRESSIONS_MOBILE_SEARCH",
			"BUSINESS_DIRECTION_REQUESTS",
			"CALL_CLICKS",
			"WEBSITE_CLICKS",
		} {
			params.Add("dailyMetrics", metric)
		}
		if err2 := c.get(endpoint, params, &resp); err2 != nil {
			return MetricaMyBusiness{}, err
		}
	}

	sums := map[string]int64{}
	for _, multi := range resp.MultiDailyMetricTimeSeries {
		for _, series := range multi.DailyMetricTimeSeries {
			var total int64
			for _, dv := range series.TimeSeries.DatedValues {
				n, _ := strconv.ParseInt(strings.TrimSpace(dv.Value), 10, 64)
				total += n
			}
			sums[series.DailyMetric] += total
		}
	}

	vistas := sums["BUSINESS_IMPRESSIONS_DESKTOP_MAPS"] +
		sums["BUSINESS_IMPRESSIONS_DESKTOP_SEARCH"] +
		sums["BUSINESS_IMPRESSIONS_MOBILE_MAPS"] +
		sums["BUSINESS_IMPRESSIONS_MOBILE_SEARCH"]
	mensajes := sums["BUSINESS_CONVERSATIONS"]
	llamadas := sums["CALL_CLICKS"]
	comoLlegar := sums["BUSINESS_DIRECTION_REQUESTS"]
	web := sums["WEBSITE_CLICKS"]

	return MetricaMyBusiness{
		Sede:                 sede.Sede,
		Mes:                  inicio,
		Vistas:               vistas,
		Mensajes:             mensajes,
		Llamadas:             llamadas,
		ComoLlegar:           comoLlegar,
		IrAlSitioWeb:         web,
		InteraccionesTotales: mensajes + llamadas + comoLlegar + web,
	}, nil
}
