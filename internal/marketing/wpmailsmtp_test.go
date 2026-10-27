package marketing

import (
	"agente_marketing/internal/domain"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agente_marketing/internal/config"
)

func TestWPMailSMTPClasificar(t *testing.T) {
	c, err := NewWPMailSMTPClient(&config.Config{EmailsLogTZ: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"Solicitud de Internet Residencial": "Internet Residencial/Residencial",
		"Solicitud de Internet Comercial":   "Internet Comercial/Comercial",
		"Solicitud de Teléfono Comercial":   "Telefonía Comercial/Comercial",
		"Solicitud de Telefonía":            "Telefonía Residencial/Residencial",
		"Verificar Cobertura":               "Cobertura/Residencial",
		"Contacto desde el Website":         "Otros/Residencial",
	}
	for subj, want := range cases {
		tipo, cli, ok := c.Clasificar(subj)
		if !ok || tipo+"/"+cli != want {
			t.Fatalf("%q: got %s/%s want %s", subj, tipo, cli, want)
		}
	}
}

func TestWPMailSMTPFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "user" || p != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("initiator") != "Contact Form 7" {
			t.Errorf("initiator=%q", r.URL.Query().Get("initiator"))
		}
		_ = json.NewEncoder(w).Encode(wpMailSMTPResponse{
			Table: "wp_wpmailsmtp_emails_log",
			Rows: []wpMailSMTPRow{
				// 2026-10-01 02:00 UTC = 2026-09-30 22:00 en Puerto Rico → septiembre
				{Hour: "2026-10-01 02:00:00", Subject: "Solicitud de Internet Residencial", Count: 2},
				{Hour: "2026-10-05 15:00:00", Subject: "Solicitud de Internet Residencial", Count: 3},
				{Hour: "2026-10-06 15:00:00", Subject: "Contacto desde el Website", Count: 1},
			},
		})
	}))
	defer srv.Close()

	c, err := NewWPMailSMTPClient(&config.Config{
		EmailsWPBaseURL: srv.URL, EmailsWPUser: "user", EmailsWPAppPassword: "pass",
		EmailsSource: "Contact Form 7", EmailsLogTZ: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Puerto_Rico")
	desde := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	hasta := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	rows, err := c.Fetch(desde, hasta, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 12 { // 2 meses × 6 tipos
		t.Fatalf("filas=%d want 12", len(rows))
	}
	got := map[string]int64{}
	for _, m := range rows {
		got[m.Mes.Format("2006-01")+"|"+m.Tipo] = m.Cantidad
	}
	want := map[string]int64{
		"2026-09|" + domain.TipoEmailInternetResidencial: 2,
		"2026-10|" + domain.TipoEmailInternetResidencial: 3,
		"2026-10|" + domain.TipoEmailOtros:               1,
		"2026-10|" + domain.TipoEmailCobertura:           0,
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s=%d want %d", k, got[k], v)
		}
	}
}
