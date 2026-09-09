package main

import (
	"fmt"
	"log"
	"time"
)

func probeGMB(cfg *Config) error {
	if !cfg.GMBEnabled() {
		return fmt.Errorf("falta GMB_REFRESH_TOKEN (y client id/secret de GMB o de Google Ads). Corre: go run . -google-oauth")
	}

	client := NewGMBClient(cfg)
	fmt.Println("=== 1) OAuth (refresh → access token) ===")
	token, err := client.ensureToken()
	if err != nil {
		return fmt.Errorf("oauth falló: %w", err)
	}
	fmt.Printf("OK — access token obtenido (%d chars)\n", len(token))

	fmt.Println("\n=== 2) Cuentas Google Business Profile ===")
	accounts, err := client.ListAccounts()
	if err != nil {
		fmt.Println("No se pudieron listar cuentas. Causas típicas:")
		fmt.Println("  - El proyecto Cloud aún no tiene acceso aprobado a GBP APIs")
		fmt.Println("  - Faltan APIs habilitadas (Account Management / Business Information / Performance)")
		fmt.Println("  - El refresh token no incluye el scope business.manage")
		fmt.Println("  - El usuario OAuth no es manager de los perfiles")
		return err
	}
	if len(accounts) == 0 {
		fmt.Println("(ninguna cuenta — el usuario no administra perfiles GBP)")
	}
	for _, a := range accounts {
		fmt.Printf("  %s  type=%s  name=%s\n", a.Name, a.Type, a.AccountName)
	}

	fmt.Println("\n=== 3) Ubicaciones (sedes) ===")
	locs, err := client.ResolveLocations()
	if err != nil {
		return err
	}
	if len(locs) == 0 {
		return fmt.Errorf("sin ubicaciones; configura GMB_LOCATION_MAP o revisa el acceso")
	}
	fmt.Printf("%-22s %-28s %s\n", "SEDE (Excel)", "LOCATION ID", "TÍTULO GBP")
	for _, l := range locs {
		fmt.Printf("%-22s %-28s %s\n", l.Sede, l.Location, l.Title)
	}
	fmt.Println("\nSi los nombres de sede no coinciden con el Excel, fija GMB_LOCATION_MAP:")
	fmt.Println("  GMB_LOCATION_MAP=Humacao:locations/111;Yauco:locations/222;Bayamón:locations/333")

	fmt.Println("\n=== 4) Métricas (rango lookback, como el sync) ===")
	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		loc = time.Local
	}
	hasta := primerDiaMes(time.Now().In(loc), loc)
	desde := hasta.AddDate(0, -(cfg.MarketingMonthsLookback - 1), 0)
	rows, err := client.Fetch(desde, hasta, loc)
	if err != nil {
		return fmt.Errorf("Fetch: %w", err)
	}
	fmt.Printf("OK — %d filas (%s → %s)\n", len(rows), desde.Format("2006-01"), hasta.Format("2006-01"))
	for _, m := range rows {
		fmt.Printf("  %s | %s | vistas=%d mensajes=%d llamadas=%d direcciones=%d web=%d total=%d\n",
			m.Mes.Format("2006-01"), m.Sede, m.Vistas, m.Mensajes, m.Llamadas, m.ComoLlegar, m.IrAlSitioWeb, m.InteraccionesTotales)
	}
	fmt.Println("\n=== Probe GMB terminado ===")
	return nil
}

func runProbeGMB() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := probeGMB(cfg); err != nil {
		log.Fatalf("probe gmb: %v", err)
	}
}
