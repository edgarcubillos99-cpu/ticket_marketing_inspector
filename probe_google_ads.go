package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// probeGoogleAds valida OAuth + acceso a la cuenta y muestra cómo se clasifican
// las campañas con ADS_RESIDENCIAL_PATTERN / ADS_COMERCIAL_PATTERN.
func probeGoogleAds(cfg *Config) error {
	if !cfg.GoogleAdsEnabled() {
		return fmt.Errorf("faltan variables GOOGLE_ADS_* (developer token, client id/secret, refresh token, customer id)")
	}

	client := NewGoogleAdsClient(cfg)

	fmt.Println("=== 1) OAuth (refresh → access token) ===")
	token, err := client.ensureToken()
	if err != nil {
		return fmt.Errorf("oauth falló: %w", err)
	}
	fmt.Printf("OK — access token obtenido (%d chars)\n", len(token))
	fmt.Printf("Customer ID: %s\n", cfg.GoogleAdsCustomerID)
	if cfg.GoogleAdsLoginCustomerID != "" {
		fmt.Printf("Login Customer ID (MCC): %s\n", cfg.GoogleAdsLoginCustomerID)
	} else {
		fmt.Println("Login Customer ID (MCC): (no configurado)")
	}
	fmt.Printf("API version: v%s\n", client.apiVersion)

	fmt.Println("\n=== 2) Campañas en la cuenta ===")
	fmt.Printf("Regex Residencial: %s\n", cfg.AdsResidencialPattern)
	fmt.Printf("Regex Comercial:   %s\n", cfg.AdsComercialPattern)

	rows, err := client.search(`
SELECT
  campaign.id,
  campaign.name,
  campaign.status
FROM campaign
WHERE campaign.status != 'REMOVED'
ORDER BY campaign.name
`)
	if err != nil {
		return fmt.Errorf("listar campañas: %w", err)
	}

	type campRow struct {
		Campaign struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"campaign"`
	}

	var (
		residencial, comercial, sinMatch int
		nombresSinMatch                  []string
	)

	fmt.Printf("\n%-12s %-12s %-14s %s\n", "ID", "STATUS", "CLASIFICACIÓN", "NOMBRE")
	fmt.Println(strings.Repeat("-", 100))

	for _, raw := range rows {
		var row campRow
		if err := json.Unmarshal(raw, &row); err != nil {
			continue
		}
		tipo := client.class.ClasificarTipoCliente(row.Campaign.Name)
		etiqueta := tipo
		switch tipo {
		case TipoClienteResidencial:
			residencial++
		case TipoClienteComercial:
			comercial++
		default:
			sinMatch++
			etiqueta = "(OMITIDA)"
			nombresSinMatch = append(nombresSinMatch, row.Campaign.Name)
		}
		fmt.Printf("%-12s %-12s %-14s %s\n", row.Campaign.ID, row.Campaign.Status, etiqueta, row.Campaign.Name)
	}

	fmt.Printf("\nResumen: total=%d | Residencial=%d | Comercial=%d | sin match (omitidas)=%d\n",
		len(rows), residencial, comercial, sinMatch)
	if len(nombresSinMatch) > 0 {
		sort.Strings(nombresSinMatch)
		fmt.Println("\nCampañas que NO matchean (el sync de Google las omite):")
		for _, n := range nombresSinMatch {
			fmt.Printf("  - %s\n", n)
		}
		fmt.Println("\nAjusta ADS_RESIDENCIAL_PATTERN / ADS_COMERCIAL_PATTERN o renombra campañas.")
	}

	fmt.Println("\n=== 3) FetchAds (último mes, como el sync) ===")
	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		loc = time.Local
	}
	ahora := time.Now().In(loc)
	desde := time.Date(ahora.Year(), ahora.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -3, 0)
	hasta := ahora

	metricas, err := client.FetchAds(desde, hasta, loc)
	if err != nil {
		return fmt.Errorf("FetchAds: %w", err)
	}
	fmt.Printf("OK — %d filas agregadas (mes %s → %s)\n",
		len(metricas), desde.Format("2006-01"), hasta.Format("2006-01"))
	for _, m := range metricas {
		fmt.Printf("  %s | %s | %s | resultado=%d | inversión=%.2f\n",
			m.Mes.Format("2006-01"), m.TipoCliente, m.TipoResultado, m.Resultado, m.Inversion)
	}
	if len(metricas) == 0 {
		fmt.Println("  (sin filas: puede ser mes sin gasto, o todas las campañas sin match de regex)")
	}

	fmt.Println("\n=== Probe terminado ===")
	return nil
}

func runProbeGoogleAds() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := probeGoogleAds(cfg); err != nil {
		log.Fatalf("probe google ads: %v", err)
	}
}
