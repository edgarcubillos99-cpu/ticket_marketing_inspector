package marketing

import (
	"agente_marketing/internal/config"
	"agente_marketing/internal/domain"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

func probeEmails(cfg *config.Config) error {
	switch cfg.EmailsProvider {
	case "":
		return fmt.Errorf("EMAILS_PROVIDER vacío. Usa EMAILS_PROVIDER=wpmailsmtp (WP Mail SMTP Pro) o gravityforms")
	case "gravityforms":
		return probeGravityForms(cfg)
	case "wpmailsmtp":
		return probeWPMailSMTP(cfg)
	default:
		return fmt.Errorf("EMAILS_PROVIDER=%q no soportado (usa wpmailsmtp o gravityforms)", cfg.EmailsProvider)
	}
}

func probeWPMailSMTP(cfg *config.Config) error {
	if !cfg.EmailsEnabled() {
		return fmt.Errorf("faltan EMAILS_WP_BASE_URL / EMAILS_WP_USER / EMAILS_WP_APP_PASSWORD")
	}
	client, err := NewWPMailSMTPClient(cfg)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		loc = time.Local
	}
	hasta := domain.PrimerDiaMes(time.Now().In(loc), loc)
	desde := hasta.AddDate(0, -(cfg.MarketingMonthsLookback - 1), 0)

	fmt.Println("=== 1) Endpoint mu-plugin (WP Mail SMTP Email Log) ===")
	fmt.Printf("URL:     %s/wp-json/osnet/v1/email-log/counts\n", client.baseURL)
	fmt.Printf("Source:  %q (EMAILS_SOURCE)\n", client.initiator)
	fmt.Printf("Log TZ:  %s → reporte en %s\n", client.logLoc, loc)

	resp, err := client.fetchRaw(desde, hasta.AddDate(0, 1, 0))
	if err != nil {
		return err
	}
	oldest := "(vacío)"
	if resp.OldestDateSent != nil {
		oldest = *resp.OldestDateSent
	}
	fmt.Printf("OK — tabla %s | correo más antiguo en el log: %s\n", resp.Table, oldest)

	fmt.Println("\n=== 2) Asuntos encontrados y su clasificación ===")
	type agg struct {
		subject, initiator string
		count              int64
	}
	bySubject := map[string]*agg{}
	for _, r := range resp.Rows {
		k := r.Initiator + "|" + r.Subject
		if a, ok := bySubject[k]; ok {
			a.count += r.Count
		} else {
			bySubject[k] = &agg{r.Subject, r.Initiator, r.Count}
		}
	}
	list := make([]*agg, 0, len(bySubject))
	for _, a := range bySubject {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].count > list[j].count })

	fmt.Printf("%-7s %-24s %-18s %s\n", "CANT", "CLASIFICACIÓN", "SOURCE", "ASUNTO")
	fmt.Println(strings.Repeat("-", 100))
	for _, a := range list {
		clas := "(omitido)"
		if tipo, cli, ok := client.Clasificar(a.subject); ok {
			clas = tipo + " / " + cli
		}
		fmt.Printf("%-7d %-24s %-18s %s\n", a.count, clas, domain.Recortar(a.initiator, 18), a.subject)
	}

	fmt.Println("\n=== 3) Recuento mensual (como el sync) ===")
	rows, err := client.Fetch(desde, hasta, loc)
	if err != nil {
		return err
	}
	for _, m := range rows {
		fmt.Printf("  %s | %-22s | %-11s | cantidad=%d\n",
			m.Mes.Format("2006-01"), m.Tipo, m.TipoCliente, m.Cantidad)
	}
	fmt.Println("\nSi algún asunto queda mal clasificado, ajusta EMAILS_SUBJECT_MAP en .env")
	fmt.Println("\n=== Probe emails terminado ===")
	return nil
}

func probeGravityForms(cfg *config.Config) error {
	if !cfg.EmailsEnabled() {
		return fmt.Errorf("faltan EMAILS_WP_BASE_URL / EMAILS_WP_USER / EMAILS_WP_APP_PASSWORD / EMAILS_FORM_MAP")
	}

	client, err := NewEmailsClient(cfg)
	if err != nil {
		return err
	}

	fmt.Println("=== 1) Formularios Gravity Forms ===")
	forms, err := client.ListForms()
	if err != nil {
		return fmt.Errorf("listar forms: %w", err)
	}
	fmt.Printf("%-8s %s\n", "ID", "TÍTULO")
	for _, f := range forms {
		fmt.Printf("%-8s %s\n", strings.TrimSpace(string(f.ID)), f.Title)
	}
	fmt.Println("\nMapeo configurado (EMAILS_FORM_MAP):")
	for _, m := range client.forms {
		fmt.Printf("  form %d → %s / %s\n", m.FormID, m.Tipo, m.TipoCliente)
	}

	fmt.Println("\n=== 2) Recuento (rango lookback) ===")
	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		loc = time.Local
	}
	hasta := domain.PrimerDiaMes(time.Now().In(loc), loc)
	desde := hasta.AddDate(0, -(cfg.MarketingMonthsLookback - 1), 0)
	rows, err := client.Fetch(desde, hasta, loc)
	if err != nil {
		return err
	}
	for _, m := range rows {
		fmt.Printf("  %s | %s | %s | cantidad=%d\n",
			m.Mes.Format("2006-01"), m.Tipo, m.TipoCliente, m.Cantidad)
	}
	fmt.Println("\n=== Probe emails terminado ===")
	return nil
}

func RunProbeEmails() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := probeEmails(cfg); err != nil {
		log.Fatalf("probe emails: %v", err)
	}
}
