package marketing

import (
	"agente_marketing/internal/config"
	"agente_marketing/internal/domain"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Mapeo por defecto asunto → Tipo/TipoCliente (se evalúa en orden, gana el primero que haga match).
// Comercial va antes que Residencial porque sus marcadores son más específicos.
const defaultWPMailSMTPSubjectMap = `Cobertura:Residencial:(?i)cobertura;` +
	`Internet Comercial:Comercial:(?i)internet.*comercial;` +
	`Telefonía Comercial:Comercial:(?i)tel[eé]f.*comercial;` +
	`Internet Residencial:Residencial:(?i)internet;` +
	`Telefonía Residencial:Residencial:(?i)tel[eé]f;` +
	`Otros:Residencial:.*`

type emailSubjectRule struct {
	Tipo        string
	TipoCliente string
	Pattern     *regexp.Regexp
}

// emailsSource abstrae el proveedor de emails (Gravity Forms o WP Mail SMTP).
type emailsSource interface {
	Fetch(desde, hasta time.Time, loc *time.Location) ([]domain.MetricaEmail, error)
}

// NewEmailsSource devuelve el cliente según EMAILS_PROVIDER.
func NewEmailsSource(cfg *config.Config) (emailsSource, error) {
	switch cfg.EmailsProvider {
	case "gravityforms":
		return NewEmailsClient(cfg)
	case "wpmailsmtp":
		return NewWPMailSMTPClient(cfg)
	default:
		return nil, fmt.Errorf("EMAILS_PROVIDER=%q no soportado (usa wpmailsmtp o gravityforms)", cfg.EmailsProvider)
	}
}

type WPMailSMTPClient struct {
	baseURL     string
	user        string
	appPassword string
	initiator   string
	logLoc      *time.Location
	rules       []emailSubjectRule
	http        *http.Client
}

func NewWPMailSMTPClient(cfg *config.Config) (*WPMailSMTPClient, error) {
	raw := cfg.EmailsSubjectMap
	if raw == "" {
		raw = defaultWPMailSMTPSubjectMap
	}
	rules, err := parseEmailSubjectMap(raw)
	if err != nil {
		return nil, err
	}
	logLoc, err := time.LoadLocation(cfg.EmailsLogTZ)
	if err != nil {
		return nil, fmt.Errorf("EMAILS_LOG_TZ %q: %w", cfg.EmailsLogTZ, err)
	}
	return &WPMailSMTPClient{
		baseURL:     strings.TrimRight(cfg.EmailsWPBaseURL, "/"),
		user:        cfg.EmailsWPUser,
		appPassword: cfg.EmailsWPAppPassword,
		initiator:   cfg.EmailsSource,
		logLoc:      logLoc,
		rules:       rules,
		http:        &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// parseEmailSubjectMap: "Tipo:TipoCliente:regex;Tipo:TipoCliente:regex;..."
func parseEmailSubjectMap(raw string) ([]emailSubjectRule, error) {
	var out []emailSubjectRule
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		bits := strings.SplitN(part, ":", 3)
		if len(bits) != 3 {
			return nil, fmt.Errorf("EMAILS_SUBJECT_MAP: entrada inválida %q (usa Tipo:TipoCliente:regex)", part)
		}
		tipo, cliente, pat := strings.TrimSpace(bits[0]), strings.TrimSpace(bits[1]), strings.TrimSpace(bits[2])
		if tipo == "" || cliente == "" || pat == "" {
			return nil, fmt.Errorf("EMAILS_SUBJECT_MAP: campos vacíos en %q", part)
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("EMAILS_SUBJECT_MAP: regex inválida en %q: %w", part, err)
		}
		out = append(out, emailSubjectRule{Tipo: tipo, TipoCliente: cliente, Pattern: re})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("EMAILS_SUBJECT_MAP no tiene entradas")
	}
	return out, nil
}

// Clasificar devuelve Tipo/TipoCliente del asunto, o ok=false si ninguna regla aplica.
func (c *WPMailSMTPClient) Clasificar(subject string) (tipo, cliente string, ok bool) {
	subject = strings.TrimSpace(subject)
	for _, r := range c.rules {
		if r.Pattern.MatchString(subject) {
			return r.Tipo, r.TipoCliente, true
		}
	}
	return "", "", false
}

type wpMailSMTPRow struct {
	Hour      string `json:"hour"`
	Subject   string `json:"subject"`
	Initiator string `json:"initiator"`
	Count     int64  `json:"count"`
}

type wpMailSMTPResponse struct {
	Table           string          `json:"table"`
	OldestDateSent  *string         `json:"oldest_date_sent"`
	InitiatorFilter string          `json:"initiator_filter"`
	Rows            []wpMailSMTPRow `json:"rows"`
}

// fetchRaw consulta el endpoint del mu-plugin osnet-email-log-api.php.
func (c *WPMailSMTPClient) fetchRaw(from, to time.Time) (*wpMailSMTPResponse, error) {
	params := url.Values{}
	params.Set("from", from.Format("2006-01-02"))
	params.Set("to", to.Format("2006-01-02"))
	if c.initiator != "" {
		params.Set("initiator", c.initiator)
	}
	endpoint := c.baseURL + "/wp-json/osnet/v1/email-log/counts?" + params.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.appPassword)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wpmailsmtp: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wpmailsmtp leer: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("wpmailsmtp HTTP 404: ¿subiste osnet-email-log-api.php a wp-content/mu-plugins/? (%s)", domain.Recortar(string(body), 200))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wpmailsmtp HTTP %d: %s", resp.StatusCode, domain.Recortar(string(body), 400))
	}
	var out wpMailSMTPResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("wpmailsmtp JSON: %w (%s)", err, domain.Recortar(string(body), 300))
	}
	return &out, nil
}

// Fetch agrega por mes (en loc) y Tipo/TipoCliente. Emite 0 para combinaciones sin correos,
// igual que la hoja EMAILS del Excel.
func (c *WPMailSMTPClient) Fetch(desde, hasta time.Time, loc *time.Location) ([]domain.MetricaEmail, error) {
	meses := domain.MesesEnRango(desde, hasta, loc)
	if len(meses) == 0 {
		return nil, nil
	}
	inicio := meses[0]
	fin := meses[len(meses)-1].AddDate(0, 1, 0)

	// Margen de un día a cada lado por la diferencia de zona horaria log ↔ loc.
	resp, err := c.fetchRaw(inicio.AddDate(0, 0, -1), fin.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}

	type key struct {
		mes, tipo, cliente string
	}
	counts := map[key]int64{}
	for _, r := range resp.Rows {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", r.Hour, c.logLoc)
		if err != nil {
			continue
		}
		t = t.In(loc)
		if t.Before(inicio) || !t.Before(fin) {
			continue
		}
		tipo, cliente, ok := c.Clasificar(r.Subject)
		if !ok {
			continue
		}
		counts[key{domain.PrimerDiaMes(t, loc).Format("2006-01"), tipo, cliente}] += r.Count
	}

	// Combinaciones únicas en el orden del mapeo.
	type combo struct{ tipo, cliente string }
	var combos []combo
	seen := map[combo]bool{}
	for _, r := range c.rules {
		cb := combo{r.Tipo, r.TipoCliente}
		if !seen[cb] {
			seen[cb] = true
			combos = append(combos, cb)
		}
	}

	var out []domain.MetricaEmail
	for _, mes := range meses {
		for _, cb := range combos {
			out = append(out, domain.MetricaEmail{
				Tipo:        cb.tipo,
				TipoCliente: cb.cliente,
				Mes:         mes,
				Cantidad:    counts[key{mes.Format("2006-01"), cb.tipo, cb.cliente}],
			})
		}
	}
	return out, nil
}
