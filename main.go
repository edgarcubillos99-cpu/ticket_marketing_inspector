// El binario queda en la raíz para que `go run .` y Docker sigan igual.
// El dominio está en internal/{config,domain,store,tickets,marketing}.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/robfig/cron/v3"

	"agente_marketing/internal/config"
	"agente_marketing/internal/marketing"
	"agente_marketing/internal/store"
	"agente_marketing/internal/tickets"
)

func main() {
	once := flag.Bool("once", false, "Ejecuta el flujo una sola vez y termina")
	backfill := flag.Bool("backfill", false, "Recorre mes a mes desde BACKFILL_FROM (2024-01-01) hasta hoy y termina")
	marketingOnly := flag.Bool("marketing-only", false, "Ejecuta solo sincronización de redes/ads y termina")
	probeGAds := flag.Bool("probe-google-ads", false, "Prueba credenciales Google Ads + clasificación Residencial/Comercial y termina")
	probeGMB := flag.Bool("probe-gmb", false, "Prueba OAuth + listado de sedes Google Business Profile y termina")
	probeEmails := flag.Bool("probe-emails", false, "Prueba Gravity Forms (lista formularios y cuenta un mes) y termina")
	googleOAuth := flag.Bool("google-oauth", false, "Abre el flujo OAuth de Google (Ads + Business Profile) y muestra el refresh token")
	flag.Parse()

	if *googleOAuth {
		marketing.RunGoogleOAuth()
		return
	}
	if *probeGAds {
		marketing.RunProbeGoogleAds()
		return
	}
	if *probeGMB {
		marketing.RunProbeGMB()
		return
	}
	if *probeEmails {
		marketing.RunProbeEmails()
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.NewStore(cfg)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	defer db.Close()

	ubersmith := tickets.NewUbersmithClient(cfg)
	analizador := tickets.NewAnalizador(cfg)

	run := func() {
		if err := tickets.EjecutarFlujo(cfg, ubersmith, analizador, db); err != nil {
			log.Printf("flujo tickets: %v", err)
		}
		if err := marketing.EjecutarFlujo(cfg, db); err != nil {
			log.Printf("flujo marketing: %v", err)
		}
	}

	if *marketingOnly {
		log.Println("Ejecutando solo marketing (redes/ads/gmb/emails)...")
		if err := marketing.EjecutarFlujo(cfg, db); err != nil {
			log.Fatalf("marketing: %v", err)
		}
		return
	}

	if *backfill || cfg.Backfill {
		log.Println("Ejecutando backfill mensual...")
		if err := tickets.EjecutarBackfill(cfg, ubersmith, analizador, db); err != nil {
			log.Fatalf("backfill: %v", err)
		}
		if err := marketing.EjecutarBackfill(cfg, db); err != nil {
			log.Fatalf("backfill marketing: %v", err)
		}
		return
	}

	if *once || cfg.RunOnStart {
		log.Println("Ejecutando flujo ahora...")
		run()
		if *once {
			return
		}
	}

	startHealthServer(cfg.Port)

	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		log.Fatalf("zona horaria %s: %v", cfg.CronTZ, err)
	}

	c := cron.New(cron.WithLocation(loc))
	if _, err := c.AddFunc(cfg.CronSpec, run); err != nil {
		log.Fatalf("cron %q: %v", cfg.CronSpec, err)
	}
	c.Start()

	entries := c.Entries()
	if len(entries) > 0 {
		log.Printf("Scheduler activo (%s %s). Próxima ejecución: %s",
			cfg.CronSpec, cfg.CronTZ, entries[0].Next.Format(time.RFC3339))
	}
	if cfg.MarketingEnabled() {
		log.Println("Marketing (redes/ads/gmb/emails): habilitado en el mismo cron semanal")
	} else {
		log.Println("Marketing (redes/ads/gmb/emails): deshabilitado hasta completar variables en .env")
	}

	select {}
}

func startHealthServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + port
	go func() {
		log.Printf("HTTP health en http://0.0.0.0%s/health", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("http: %v", err)
		}
	}()
}
