package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type LinkedInClient struct {
	token    string
	orgID    string
	adAcctID string
	version  string
	http     *http.Client
	class    *clasificadorCliente
}

func NewLinkedInClient(cfg *Config) *LinkedInClient {
	return &LinkedInClient{
		token:    cfg.LinkedInAccessToken,
		orgID:    cfg.LinkedInOrganizationID,
		adAcctID: cfg.LinkedInAdAccountID,
		version:  cfg.LinkedInAPIVersion,
		http:     &http.Client{Timeout: 120 * time.Second},
		class:    newClasificadorCliente(cfg.AdsResidencialPattern, cfg.AdsComercialPattern),
	}
}

func (c *LinkedInClient) orgURN() string {
	id := strings.TrimPrefix(c.orgID, "urn:li:organization:")
	return "urn:li:organization:" + id
}

func (c *LinkedInClient) get(endpoint string, params url.Values, out any) error {
	if params != nil && len(params) > 0 {
		if strings.Contains(endpoint, "?") {
			endpoint += "&" + params.Encode()
		} else {
			endpoint += "?" + params.Encode()
		}
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("LinkedIn-Version", c.version)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("linkedin leer: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("linkedin HTTP %d: %s", resp.StatusCode, recortar(string(body), 400))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("linkedin JSON: %w (%s)", err, recortar(string(body), 300))
	}
	return nil
}

// FetchOrganico obtiene share + follower statistics mensuales de la página de empresa.
func (c *LinkedInClient) FetchOrganico(desde, hasta time.Time, loc *time.Location) ([]MetricaRedSocial, error) {
	meses := mesesEnRango(desde, hasta, loc)
	out := make([]MetricaRedSocial, 0, len(meses))

	for _, mes := range meses {
		m, err := c.mesOrganico(mes, loc)
		if err != nil {
			return nil, fmt.Errorf("linkedin organico %s: %w", mes.Format("2006-01"), err)
		}
		out = append(out, m)
	}
	return out, nil
}

func (c *LinkedInClient) mesOrganico(mes time.Time, loc *time.Location) (MetricaRedSocial, error) {
	inicio := primerDiaMes(mes, loc)
	fin := inicio.AddDate(0, 1, 0)
	m := MetricaRedSocial{
		Plataforma: PlataformaLinkedIn,
		Mes:        inicio,
	}

	org := url.QueryEscape(c.orgURN())
	timeRange := fmt.Sprintf(
		"(timeRange:(start:%d,end:%d),timeGranularityType:MONTH)",
		inicio.UnixMilli(), fin.UnixMilli(),
	)

	// Share statistics (impresiones, likes, comments, shares, engagement).
	shareURL := "https://api.linkedin.com/rest/organizationalEntityShareStatistics" +
		"?q=organizationalEntity&organizationalEntity=" + org +
		"&timeIntervals=" + url.QueryEscape(timeRange)

	var shareResp struct {
		Elements []struct {
			TotalShareStatistics struct {
				ImpressionCount  int64 `json:"impressionCount"`
				UniqueImpressionsCount int64 `json:"uniqueImpressionsCount"`
				LikeCount        int64 `json:"likeCount"`
				CommentCount     int64 `json:"commentCount"`
				ShareCount       int64 `json:"shareCount"`
				ClickCount       int64 `json:"clickCount"`
				Engagement       float64 `json:"engagement"`
			} `json:"totalShareStatistics"`
		} `json:"elements"`
	}
	if err := c.get(shareURL, nil, &shareResp); err != nil {
		return m, err
	}
	if len(shareResp.Elements) > 0 {
		s := shareResp.Elements[0].TotalShareStatistics
		if s.UniqueImpressionsCount > 0 {
			m.Alcance = s.UniqueImpressionsCount
		} else {
			m.Alcance = s.ImpressionCount
		}
		m.LikesReacciones = s.LikeCount
		m.Comentarios = s.CommentCount
		m.Compartir = s.ShareCount
		m.TotalInteracciones = sumInteracciones(s.LikeCount, s.CommentCount, s.ShareCount)
	}

	// Follower statistics (netos del periodo + total al cierre si está disponible).
	followerURL := "https://api.linkedin.com/rest/organizationalEntityFollowerStatistics" +
		"?q=organizationalEntity&organizationalEntity=" + org +
		"&timeIntervals=" + url.QueryEscape(timeRange)

	var followerResp struct {
		Elements []struct {
			FollowerGains struct {
				OrganicFollowerGain int64 `json:"organicFollowerGain"`
				PaidFollowerGain    int64 `json:"paidFollowerGain"`
			} `json:"followerGains"`
		} `json:"elements"`
	}
	if err := c.get(followerURL, nil, &followerResp); err == nil && len(followerResp.Elements) > 0 {
		g := followerResp.Elements[0].FollowerGains
		m.SeguidoresNetos = g.OrganicFollowerGain + g.PaidFollowerGain
	}

	// Total de seguidores actuales de la organización.
	totalURL := "https://api.linkedin.com/rest/networkSizes/" + url.PathEscape(c.orgURN()) +
		"?edgeType=COMPANY_FOLLOWED_BY_MEMBER"
	var totalResp struct {
		FirstDegreeSize int64 `json:"firstDegreeSize"`
	}
	if err := c.get(totalURL, nil, &totalResp); err == nil {
		m.TotalSeguidores = totalResp.FirstDegreeSize
	}

	return m, nil
}

// FetchAds obtiene analytics mensuales de LinkedIn Campaign Manager.
func (c *LinkedInClient) FetchAds(desde, hasta time.Time, loc *time.Location) ([]MetricaAnuncio, error) {
	if c.adAcctID == "" {
		return nil, nil
	}

	inicio := primerDiaMes(desde, loc)
	fin := primerDiaMes(hasta, loc).AddDate(0, 1, 0)
	accountURN := "urn:li:sponsoredAccount:" + strings.TrimPrefix(c.adAcctID, "urn:li:sponsoredAccount:")

	// Lista campañas para clasificar Residencial/Comercial por nombre.
	campañas, err := c.listarCampañas(accountURN)
	if err != nil {
		return nil, err
	}

	agg := map[string]*MetricaAnuncio{}

	for campID, nombre := range campañas {
		tipoCliente := c.class.ClasificarTipoCliente(nombre)
		if tipoCliente == "" {
			// En el reporte histórico LinkedIn aparece solo como Comercial.
			tipoCliente = TipoClienteComercial
		}

		analyticsURL := "https://api.linkedin.com/rest/adAnalytics" +
			"?q=analytics" +
			"&pivot=CAMPAIGN" +
			"&timeGranularity=MONTHLY" +
			"&dateRange=" + url.QueryEscape(fmt.Sprintf("(start:(year:%d,month:%d,day:1),end:(year:%d,month:%d,day:1))",
				inicio.Year(), int(inicio.Month()), fin.Year(), int(fin.Month()))) +
			"&campaigns=List(" + url.QueryEscape("urn:li:sponsoredCampaign:"+campID) + ")" +
			"&fields=clicks,costInLocalCurrency,dateRange,pivotValues"

		var resp struct {
			Elements []struct {
				Clicks              int64  `json:"clicks"`
				CostInLocalCurrency string `json:"costInLocalCurrency"`
				DateRange           struct {
					Start struct {
						Year  int `json:"year"`
						Month int `json:"month"`
						Day   int `json:"day"`
					} `json:"start"`
				} `json:"dateRange"`
			} `json:"elements"`
		}
		if err := c.get(analyticsURL, nil, &resp); err != nil {
			return nil, fmt.Errorf("adAnalytics campaña %s: %w", campID, err)
		}

		for _, el := range resp.Elements {
			mes := time.Date(el.DateRange.Start.Year, time.Month(el.DateRange.Start.Month), 1, 0, 0, 0, 0, loc)
			spend, _ := strconv.ParseFloat(el.CostInLocalCurrency, 64)
			key := PlataformaLinkedIn + "|" + tipoCliente + "|" + mes.Format("2006-01") + "|" + TipoResultadoClics
			if cur, ok := agg[key]; ok {
				cur.Resultado += el.Clicks
				cur.Inversion += spend
			} else {
				agg[key] = &MetricaAnuncio{
					Plataforma:    PlataformaLinkedIn,
					TipoCliente:   tipoCliente,
					Mes:           mes,
					TipoResultado: TipoResultadoClics,
					Resultado:     el.Clicks,
					Inversion:     spend,
				}
			}
		}
	}

	out := make([]MetricaAnuncio, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	return out, nil
}

func (c *LinkedInClient) listarCampañas(accountURN string) (map[string]string, error) {
	endpoint := "https://api.linkedin.com/rest/adAccounts/" +
		url.PathEscape(strings.TrimPrefix(accountURN, "urn:li:sponsoredAccount:")) +
		"/adCampaigns?q=search&search=(status:(values:List(ACTIVE,PAUSED,COMPLETED,ARCHIVED)))&pageSize=1000"

	var resp struct {
		Elements []struct {
			ID     json.RawMessage `json:"id"`
			Name   string          `json:"name"`
			IDStr  string          `json:"-"`
		} `json:"elements"`
	}
	if err := c.get(endpoint, nil, &resp); err != nil {
		// Fallback REST li search
		endpoint = "https://api.linkedin.com/rest/adCampaigns?q=search&search=(account:(values:List(" +
			url.QueryEscape(accountURN) + ")))&pageSize=1000"
		if err2 := c.get(endpoint, nil, &resp); err2 != nil {
			return nil, fmt.Errorf("listar campañas: %v / %w", err, err2)
		}
	}

	out := make(map[string]string, len(resp.Elements))
	for _, el := range resp.Elements {
		id := strings.TrimSpace(string(el.ID))
		id = strings.Trim(id, `"`)
		if id == "" {
			continue
		}
		out[id] = el.Name
	}
	return out, nil
}
