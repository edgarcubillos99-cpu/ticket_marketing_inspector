package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func probeEmails(cfg *Config) error {
	if cfg.EmailsProvider == "" {
		return fmt.Errorf("EMAILS_PROVIDER vacío. Cuando sepas el plugin, usa EMAILS_PROVIDER=gravityforms (si aplica) más URL, usuario WP y EMAILS_FORM_MAP")
	}
	if cfg.EmailsProvider != "gravityforms" {
		return fmt.Errorf("EMAILS_PROVIDER=%q no implementado aún (solo gravityforms). Identifica el plugin con tu jefe", cfg.EmailsProvider)
	}
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
	hasta := primerDiaMes(time.Now().In(loc), loc)
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

func runProbeEmails() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := probeEmails(cfg); err != nil {
		log.Fatalf("probe emails: %v", err)
	}
}
