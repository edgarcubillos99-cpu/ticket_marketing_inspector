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

type MetaClient struct {
	token    string
	version  string
	pageID   string
	igUserID string
	adAcctID string
	http     *http.Client
	class    *clasificadorCliente
}

func NewMetaClient(cfg *Config) *MetaClient {
	return &MetaClient{
		token:    cfg.MetaAccessToken,
		version:  strings.TrimPrefix(cfg.MetaAPIVersion, "/"),
		pageID:   cfg.FacebookPageID,
		igUserID: cfg.InstagramBusinessAccountID,
		adAcctID: cfg.MetaAdAccountID,
		http:     &http.Client{Timeout: 120 * time.Second},
		class:    newClasificadorCliente(cfg.AdsResidencialPattern, cfg.AdsComercialPattern),
	}
}

func (c *MetaClient) base() string {
	return "https://graph.facebook.com/" + c.version
}

func (c *MetaClient) get(path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("access_token", c.token)

	endpoint := c.base() + path
	if strings.Contains(endpoint, "?") {
		endpoint += "&" + params.Encode()
	} else {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("meta leer respuesta: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("meta HTTP %d: %s", resp.StatusCode, recortar(string(body), 400))
	}

	var apiErr struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &apiErr)
	if apiErr.Error != nil && apiErr.Error.Message != "" {
		return fmt.Errorf("meta API: %s (code %d)", apiErr.Error.Message, apiErr.Error.Code)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("meta JSON: %w (%s)", err, recortar(string(body), 300))
	}
	return nil
}

// FetchOrganico obtiene métricas mensuales de Facebook Page e Instagram Business.
func (c *MetaClient) FetchOrganico(desde, hasta time.Time, loc *time.Location) ([]MetricaRedSocial, error) {
	var out []MetricaRedSocial
	var errs []string
	meses := mesesEnRango(desde, hasta, loc)

	if c.pageID != "" {
		for _, mes := range meses {
			m, err := c.facebookMes(mes, loc)
			if err != nil {
				errs = append(errs, fmt.Sprintf("facebook %s: %v", mes.Format("2006-01"), err))
				continue
			}
			out = append(out, m)
		}
	}

	if c.igUserID != "" {
		for _, mes := range meses {
			m, err := c.instagramMes(mes, loc)
			if err != nil {
				errs = append(errs, fmt.Sprintf("instagram %s: %v", mes.Format("2006-01"), err))
				continue
			}
			out = append(out, m)
		}
	}

	if len(errs) == 0 {
		return out, nil
	}
	joined := strings.Join(errs, "; ")
	if len(out) == 0 {
		return nil, fmt.Errorf("%s", joined)
	}
	return out, fmt.Errorf("%s", joined)
}

// rangoInsightsMes acota [inicio, fin) al mes y al instante actual (Meta rechaza since futuro).
func rangoInsightsMes(mes time.Time, loc *time.Location) (inicio, fin time.Time, ok bool) {
	inicio = primerDiaMes(mes, loc)
	fin = inicio.AddDate(0, 1, 0)
	now := time.Now().In(loc)
	if !inicio.Before(now) {
		return inicio, fin, false
	}
	if fin.After(now) {
		fin = now
	}
	return inicio, fin, inicio.Before(fin)
}

func (c *MetaClient) facebookMes(mes time.Time, loc *time.Location) (MetricaRedSocial, error) {
	inicio, fin, ok := rangoInsightsMes(mes, loc)
	if !ok {
		return MetricaRedSocial{}, fmt.Errorf("sin datos disponibles aún para el mes")
	}
	since := strconv.FormatInt(inicio.Unix(), 10)
	until := strconv.FormatInt(fin.Unix(), 10)

	m := MetricaRedSocial{
		Plataforma: PlataformaFacebook,
		Mes:        primerDiaMes(mes, loc),
	}

	// Alcance (viewers únicos) + follows al cierre del periodo.
	// page_impressions_* / page_fans / page_fan_* fueron deprecados (nov 2025).
	type insightResp struct {
		Data []struct {
			Name   string `json:"name"`
			Period string `json:"period"`
			Values []struct {
				Value   json.RawMessage `json:"value"`
				EndTime string          `json:"end_time"`
			} `json:"values"`
		} `json:"data"`
	}

	var insights insightResp
	params := url.Values{}
	params.Set("metric", "page_total_media_view_unique,page_follows")
	params.Set("period", "day")
	params.Set("since", since)
	params.Set("until", until)
	if err := c.get("/"+c.pageID+"/insights", params, &insights); err != nil {
		return m, err
	}

	for _, d := range insights.Data {
		switch d.Name {
		case "page_total_media_view_unique":
			m.Alcance = sumInsightValues(d.Values)
		case "page_follows":
			if len(d.Values) == 0 {
				continue
			}
			first := parseJSONInt(d.Values[0].Value)
			last := parseJSONInt(d.Values[len(d.Values)-1].Value)
			m.TotalSeguidores = last
			m.SeguidoresNetos = last - first
		}
	}

	likes, comments, shares, err := c.agregarEngagementPostsFacebook(inicio, fin)
	if err != nil {
		return m, err
	}
	m.LikesReacciones = likes
	m.Comentarios = comments
	m.Compartir = shares
	m.TotalInteracciones = sumInteracciones(likes, comments, shares)
	return m, nil
}

func (c *MetaClient) agregarEngagementPostsFacebook(inicio, fin time.Time) (likes, comments, shares int64, err error) {
	params := url.Values{}
	params.Set("fields", "id,created_time,likes.summary(true).limit(0),comments.summary(true).limit(0),shares")
	params.Set("since", strconv.FormatInt(inicio.Unix(), 10))
	params.Set("until", strconv.FormatInt(fin.Unix(), 10))
	params.Set("limit", "100")

	path := "/" + c.pageID + "/published_posts"
	for path != "" {
		var page struct {
			Data []struct {
				Likes *struct {
					Summary struct {
						TotalCount int64 `json:"total_count"`
					} `json:"summary"`
				} `json:"likes"`
				Comments *struct {
					Summary struct {
						TotalCount int64 `json:"total_count"`
					} `json:"summary"`
				} `json:"comments"`
				Shares *struct {
					Count int64 `json:"count"`
				} `json:"shares"`
			} `json:"data"`
			Paging *struct {
				Next string `json:"next"`
			} `json:"paging"`
		}

		if strings.HasPrefix(path, "http") {
			if err := c.getAbsolute(path, &page); err != nil {
				return 0, 0, 0, err
			}
		} else {
			if err := c.get(path, params, &page); err != nil {
				return 0, 0, 0, err
			}
			params = nil // solo en la primera página
		}

		for _, p := range page.Data {
			if p.Likes != nil {
				likes += p.Likes.Summary.TotalCount
			}
			if p.Comments != nil {
				comments += p.Comments.Summary.TotalCount
			}
			if p.Shares != nil {
				shares += p.Shares.Count
			}
		}

		if page.Paging != nil && page.Paging.Next != "" {
			path = page.Paging.Next
		} else {
			path = ""
		}
	}
	return likes, comments, shares, nil
}

// Instagram Insights rechaza ventanas > 30 días entre since y until.
const maxIGInsightSpan = 30 * 24 * time.Hour

func (c *MetaClient) instagramMes(mes time.Time, loc *time.Location) (MetricaRedSocial, error) {
	inicio, fin, ok := rangoInsightsMes(mes, loc)
	if !ok {
		return MetricaRedSocial{}, fmt.Errorf("sin datos disponibles aún para el mes")
	}

	m := MetricaRedSocial{
		Plataforma: PlataformaInstagram,
		Mes:        primerDiaMes(mes, loc),
	}

	// Reach diario. follower_count (insights) = altas nuevas por día, NO el total.
	// Total actual: campo followers_count del IG User.
	type insightResp struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value   json.RawMessage `json:"value"`
				EndTime string          `json:"end_time"`
			} `json:"values"`
		} `json:"data"`
	}

	var profile struct {
		FollowersCount int64 `json:"followers_count"`
	}
	if err := c.get("/"+c.igUserID, url.Values{"fields": []string{"followers_count"}}, &profile); err == nil {
		m.TotalSeguidores = profile.FollowersCount
	}

	for _, w := range insightWindows(inicio, fin, maxIGInsightSpan) {
		since := strconv.FormatInt(w[0].Unix(), 10)
		until := strconv.FormatInt(w[1].Unix(), 10)

		var reach insightResp
		params := url.Values{}
		params.Set("metric", "reach")
		params.Set("period", "day")
		params.Set("since", since)
		params.Set("until", until)
		if err := c.get("/"+c.igUserID+"/insights", params, &reach); err != nil {
			return m, err
		}
		for _, d := range reach.Data {
			if d.Name == "reach" {
				m.Alcance += sumInsightValues(d.Values)
			}
		}

		var followers insightResp
		fp := url.Values{}
		fp.Set("metric", "follower_count")
		fp.Set("period", "day")
		fp.Set("since", since)
		fp.Set("until", until)
		if err := c.get("/"+c.igUserID+"/insights", fp, &followers); err != nil {
			continue
		}
		for _, d := range followers.Data {
			if d.Name == "follower_count" {
				m.SeguidoresNetos += sumInsightValues(d.Values)
			}
		}
	}

	likes, comments, shares, err := c.agregarEngagementMediaInstagram(inicio, fin)
	if err != nil {
		return m, err
	}
	m.LikesReacciones = likes
	m.Comentarios = comments
	m.Compartir = shares
	m.TotalInteracciones = sumInteracciones(likes, comments, shares)
	return m, nil
}

// insightWindows parte [inicio, fin) en tramos de a lo sumo maxSpan (límite de IG Insights).
func insightWindows(inicio, fin time.Time, maxSpan time.Duration) [][2]time.Time {
	if !inicio.Before(fin) || maxSpan <= 0 {
		return nil
	}
	var out [][2]time.Time
	cur := inicio
	for cur.Before(fin) {
		next := cur.Add(maxSpan)
		if next.After(fin) {
			next = fin
		}
		out = append(out, [2]time.Time{cur, next})
		cur = next
	}
	return out
}

func (c *MetaClient) agregarEngagementMediaInstagram(inicio, fin time.Time) (likes, comments, shares int64, err error) {
	// No pedir insights embebidos: falla (#2108006) si el media es anterior a la
	// conversión a cuenta Business. likes/comments vienen del objeto; shares aparte.
	params := url.Values{}
	params.Set("fields", "id,timestamp,like_count,comments_count")
	params.Set("limit", "100")

	path := "/" + c.igUserID + "/media"
	for path != "" {
		var page struct {
			Data []struct {
				ID            string `json:"id"`
				Timestamp     string `json:"timestamp"`
				LikeCount     int64  `json:"like_count"`
				CommentsCount int64  `json:"comments_count"`
			} `json:"data"`
			Paging *struct {
				Next string `json:"next"`
			} `json:"paging"`
		}

		if strings.HasPrefix(path, "http") {
			if err := c.getAbsolute(path, &page); err != nil {
				return 0, 0, 0, err
			}
		} else {
			if err := c.get(path, params, &page); err != nil {
				return 0, 0, 0, err
			}
			params = nil
		}

		for _, media := range page.Data {
			ts, parseErr := parseMetaTime(media.Timestamp)
			if parseErr != nil {
				continue
			}
			if ts.Before(inicio) || !ts.Before(fin) {
				continue
			}
			likes += media.LikeCount
			comments += media.CommentsCount
			shares += c.igMediaShares(media.ID)
		}

		if page.Paging != nil && page.Paging.Next != "" {
			path = page.Paging.Next
		} else {
			path = ""
		}
	}
	return likes, comments, shares, nil
}

// igMediaShares intenta shares del media; 0 si no aplica (stories, pre-Business, etc.).
func (c *MetaClient) igMediaShares(mediaID string) int64 {
	if mediaID == "" {
		return 0
	}
	type insightResp struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value json.RawMessage `json:"value"`
			} `json:"values"`
		} `json:"data"`
	}
	var out insightResp
	params := url.Values{}
	params.Set("metric", "shares")
	if err := c.get("/"+mediaID+"/insights", params, &out); err != nil {
		return 0
	}
	for _, d := range out.Data {
		if d.Name == "shares" && len(d.Values) > 0 {
			return parseJSONInt(d.Values[0].Value)
		}
	}
	return 0
}

// FetchAds obtiene insights mensuales de Meta Ads (Facebook + Instagram por publisher_platform).
func (c *MetaClient) FetchAds(desde, hasta time.Time, loc *time.Location) ([]MetricaAnuncio, error) {
	if c.adAcctID == "" {
		return nil, nil
	}

	inicio := primerDiaMes(desde, loc)
	finExcl := primerDiaMes(hasta, loc).AddDate(0, 1, 0)

	params := url.Values{}
	// publisher_platform solo en breakdowns (no en fields).
	params.Set("fields", "campaign_name,spend,clicks,actions")
	params.Set("level", "campaign")
	params.Set("time_increment", "monthly")
	params.Set("breakdowns", "publisher_platform")
	params.Set("time_range", fmt.Sprintf(`{"since":"%s","until":"%s"}`,
		inicio.Format("2006-01-02"), finExcl.AddDate(0, 0, -1).Format("2006-01-02")))
	params.Set("limit", "500")

	type row struct {
		CampaignName      string `json:"campaign_name"`
		Spend             string `json:"spend"`
		Clicks            string `json:"clicks"`
		PublisherPlatform string `json:"publisher_platform"`
		DateStart         string `json:"date_start"`
		Actions           []struct {
			ActionType string `json:"action_type"`
			Value      string `json:"value"`
		} `json:"actions"`
	}

	agg := map[string]*MetricaAnuncio{}

	path := "/act_" + c.adAcctID + "/insights"
	for path != "" {
		var page struct {
			Data   []row `json:"data"`
			Paging *struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		if strings.HasPrefix(path, "http") {
			if err := c.getAbsolute(path, &page); err != nil {
				return nil, err
			}
		} else {
			if err := c.get(path, params, &page); err != nil {
				return nil, err
			}
			params = nil
		}

		for _, r := range page.Data {
			tipoCliente := c.class.ClasificarTipoCliente(r.CampaignName)
			if tipoCliente == "" {
				// Campañas Meta de Osnet no usan "Residencial/Comercial" en el nombre;
				// por defecto Residencial (LinkedIn defaulta Comercial).
				tipoCliente = TipoClienteResidencial
			}
			plataforma := mapPublisherPlatform(r.PublisherPlatform)
			if plataforma == "" {
				continue
			}
			mes, err := time.ParseInLocation("2006-01-02", r.DateStart, loc)
			if err != nil {
				continue
			}
			mes = primerDiaMes(mes, loc)

			tipoResultado, resultado := metaResultado(plataforma, tipoCliente, r.Clicks, toMetaActions(r.Actions))
			spend, _ := strconv.ParseFloat(r.Spend, 64)

			key := plataforma + "|" + tipoCliente + "|" + mes.Format("2006-01") + "|" + tipoResultado
			if cur, ok := agg[key]; ok {
				cur.Resultado += resultado
				cur.Inversion += spend
			} else {
				agg[key] = &MetricaAnuncio{
					Plataforma:    plataforma,
					TipoCliente:   tipoCliente,
					Mes:           mes,
					TipoResultado: tipoResultado,
					Resultado:     resultado,
					Inversion:     spend,
				}
			}
		}

		if page.Paging != nil && page.Paging.Next != "" {
			path = page.Paging.Next
		} else {
			path = ""
		}
	}

	out := make([]MetricaAnuncio, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	return out, nil
}

func mapPublisherPlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "facebook":
		return PlataformaFacebook
	case "instagram":
		return PlataformaInstagram
	case "audience_network", "messenger", "unknown", "":
		return ""
	default:
		return ""
	}
}

type metaAction struct {
	ActionType string
	Value      string
}

func toMetaActions(in []struct {
	ActionType string `json:"action_type"`
	Value      string `json:"value"`
}) []metaAction {
	out := make([]metaAction, 0, len(in))
	for _, a := range in {
		out = append(out, metaAction{ActionType: a.ActionType, Value: a.Value})
	}
	return out
}

func metaResultado(plataforma, tipoCliente, clicks string, actions []metaAction) (tipo string, valor int64) {
	// Convención del reporte de la empresa:
	// Facebook Residencial → Mensajes; Instagram → Interacciones; resto → Clics.
	if plataforma == PlataformaFacebook && tipoCliente == TipoClienteResidencial {
		for _, a := range actions {
			switch a.ActionType {
			case "onsite_conversion.messaging_conversation_started_7d",
				"onsite_conversion.total_messaging_connection",
				"onsite_conversion.messaging_first_reply":
				n, _ := strconv.ParseInt(a.Value, 10, 64)
				valor += n
			}
		}
		return TipoResultadoMensajes, valor
	}
	if plataforma == PlataformaInstagram {
		for _, a := range actions {
			if a.ActionType == "post_engagement" || a.ActionType == "post_interaction_gross" {
				n, _ := strconv.ParseInt(a.Value, 10, 64)
				valor += n
			}
		}
		if valor == 0 {
			valor, _ = strconv.ParseInt(clicks, 10, 64)
		}
		return TipoResultadoInteracciones, valor
	}
	valor, _ = strconv.ParseInt(clicks, 10, 64)
	return TipoResultadoClics, valor
}

func (c *MetaClient) getAbsolute(fullURL string, out any) error {
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("meta HTTP %d: %s", resp.StatusCode, recortar(string(body), 400))
	}
	return json.Unmarshal(body, out)
}

func sumInsightValues(values []struct {
	Value   json.RawMessage `json:"value"`
	EndTime string          `json:"end_time"`
}) int64 {
	var sum int64
	for _, v := range values {
		sum += parseJSONInt(v.Value)
	}
	return sum
}

func parseJSONInt(raw json.RawMessage) int64 {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return n
	}
	// Algunos insights vienen como objeto {like:1, love:2}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		var sum int64
		for _, v := range obj {
			switch t := v.(type) {
			case float64:
				sum += int64(t)
			case string:
				n, _ := strconv.ParseInt(t, 10, 64)
				sum += n
			}
		}
		return sum
	}
	return 0
}

// parseMetaTime acepta timestamps Graph (+0000) y RFC3339 (+00:00).
func parseMetaTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05Z0700", s)
}
