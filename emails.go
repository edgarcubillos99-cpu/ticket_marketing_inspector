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

type emailFormMap struct {
	FormID      int
	Tipo        string
	TipoCliente string
}

type EmailsClient struct {
	baseURL     string
	user        string
	appPassword string
	forms       []emailFormMap
	http        *http.Client
}

func NewEmailsClient(cfg *Config) (*EmailsClient, error) {
	forms, err := parseEmailsFormMap(cfg.EmailsFormMap)
	if err != nil {
		return nil, err
	}
	return &EmailsClient{
		baseURL:     strings.TrimRight(cfg.EmailsWPBaseURL, "/"),
		user:        cfg.EmailsWPUser,
		appPassword: cfg.EmailsWPAppPassword,
		forms:       forms,
		http:        &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func parseEmailsFormMap(raw string) ([]emailFormMap, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("EMAILS_FORM_MAP vacío")
	}
	var out []emailFormMap
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bits := strings.Split(part, ":")
		if len(bits) < 3 {
			return nil, fmt.Errorf("entrada inválida %q (usa formID:Tipo:TipoCliente)", part)
		}
		id, err := strconv.Atoi(strings.TrimSpace(bits[0]))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("form ID inválido en %q", part)
		}
		tipo := strings.TrimSpace(strings.Join(bits[1:len(bits)-1], ":"))
		cliente := strings.TrimSpace(bits[len(bits)-1])
		if tipo == "" || cliente == "" {
			return nil, fmt.Errorf("tipo/tipo_cliente vacío en %q", part)
		}
		out = append(out, emailFormMap{FormID: id, Tipo: tipo, TipoCliente: cliente})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("EMAILS_FORM_MAP no tiene entradas")
	}
	return out, nil
}

func (c *EmailsClient) get(path string, params url.Values, out any) error {
	endpoint := c.baseURL + path
	if params != nil && len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.appPassword)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("emails leer: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("emails HTTP %d: %s", resp.StatusCode, recortar(string(body), 400))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("emails JSON: %w (%s)", err, recortar(string(body), 300))
	}
	return nil
}

type gravityForm struct {
	ID    json.RawMessage `json:"id"`
	Title string          `json:"title"`
}

func (c *EmailsClient) ListForms() ([]gravityForm, error) {
	var raw json.RawMessage
	if err := c.get("/wp-json/gf/v2/forms", nil, &raw); err != nil {
		return nil, err
	}
	var forms []gravityForm
	if err := json.Unmarshal(raw, &forms); err == nil {
		return forms, nil
	}
	var obj map[string]gravityForm
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("emails forms JSON: %w (%s)", err, recortar(string(raw), 300))
	}
	out := make([]gravityForm, 0, len(obj))
	for _, f := range obj {
		out = append(out, f)
	}
	return out, nil
}

func (c *EmailsClient) Fetch(desde, hasta time.Time, loc *time.Location) ([]MetricaEmail, error) {
	meses := mesesEnRango(desde, hasta, loc)
	var out []MetricaEmail
	for _, form := range c.forms {
		for _, mes := range meses {
			n, err := c.contarEntradasMes(form.FormID, mes, loc)
			if err != nil {
				return nil, fmt.Errorf("form %d %s: %w", form.FormID, mes.Format("2006-01"), err)
			}
			out = append(out, MetricaEmail{
				Tipo:        form.Tipo,
				TipoCliente: form.TipoCliente,
				Mes:         primerDiaMes(mes, loc),
				Cantidad:    n,
			})
		}
	}
	return out, nil
}

func (c *EmailsClient) contarEntradasMes(formID int, mes time.Time, loc *time.Location) (int64, error) {
	inicio := primerDiaMes(mes, loc)
	fin := inicio.AddDate(0, 1, 0).Add(-time.Second)
	search, _ := json.Marshal(map[string]string{
		"start_date": inicio.Format("2006-01-02 15:04:05"),
		"end_date":   fin.Format("2006-01-02 15:04:05"),
	})

	var total int64
	page := 1
	for {
		params := url.Values{}
		params.Set("form_ids", strconv.Itoa(formID))
		params.Set("search", string(search))
		params.Set("paging[page_size]", "200")
		params.Set("paging[current_page]", strconv.Itoa(page))

		var payload json.RawMessage
		if err := c.get("/wp-json/gf/v2/entries", params, &payload); err != nil {
			return 0, err
		}

		count, more, err := gravityEntriesCount(payload)
		if err != nil {
			return 0, err
		}
		total += count
		if !more {
			break
		}
		page++
		if page > 500 {
			return 0, fmt.Errorf("demasiadas páginas de entradas (form %d)", formID)
		}
	}
	return total, nil
}

func gravityEntriesCount(payload json.RawMessage) (count int64, more bool, err error) {
	var obj struct {
		TotalCount json.RawMessage   `json:"total_count"`
		Entries    []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(payload, &obj); err == nil && (len(obj.Entries) > 0 || len(obj.TotalCount) > 0) {
		if n := parseJSONInt(obj.TotalCount); n > 0 {
			return n, false, nil
		}
		return int64(len(obj.Entries)), len(obj.Entries) >= 200, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(payload, &arr); err != nil {
		return 0, false, fmt.Errorf("formato de entries no reconocido")
	}
	return int64(len(arr)), len(arr) >= 200, nil
}
