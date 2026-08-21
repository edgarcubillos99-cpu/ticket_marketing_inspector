package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

func main() {
	once := flag.Bool("once", false, "Ejecuta el flujo una sola vez y termina")
	backfill := flag.Bool("backfill", false, "Recorre mes a mes desde BACKFILL_FROM (2024-01-01) hasta hoy y termina")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := NewStore(cfg)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}
	defer store.Close()

	ubersmith := NewUbersmithClient(cfg)
	analizador := NewAnalizador(cfg)

	run := func() {
		if err := ejecutarFlujo(cfg, ubersmith, analizador, store); err != nil {
			log.Printf("flujo: %v", err)
		}
	}

	if *backfill || cfg.Backfill {
		log.Println("Ejecutando backfill mensual...")
		if err := ejecutarBackfill(cfg, ubersmith, analizador, store); err != nil {
			log.Fatalf("backfill: %v", err)
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

func ejecutarBackfill(cfg *Config, api *UbersmithClient, ia *Analizador, store *Store) error {
	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		return fmt.Errorf("zona horaria %s: %w", cfg.CronTZ, err)
	}

	desde, err := time.ParseInLocation("2006-01-02", cfg.BackfillFrom, loc)
	if err != nil {
		return fmt.Errorf("BACKFILL_FROM %q: usa YYYY-MM-DD", cfg.BackfillFrom)
	}

	hasta := time.Now().In(loc)
	periodos := periodosMensuales(desde, hasta, loc)
	log.Printf("Backfill: %d periodos de %s a %s (%s)",
		len(periodos), desde.Format("2006-01-02"), hasta.Format("2006-01-02 15:04:05"), loc)

	okTotal, failTotal := 0, 0
	for i, p := range periodos {
		log.Printf("Periodo %d/%d: %s → %s", i+1, len(periodos),
			p[0].Format("2006-01-02 15:04:05"), p[1].Format("2006-01-02 15:04:05"))
		ok, fail, err := procesarRango(cfg, api, ia, store, p[0], p[1])
		if err != nil {
			return fmt.Errorf("periodo %s: %w", p[0].Format("2006-01"), err)
		}
		okTotal += ok
		failTotal += fail
	}

	log.Printf("Backfill completado. OK=%d errores=%d", okTotal, failTotal)
	return nil
}

func ejecutarFlujo(cfg *Config, api *UbersmithClient, ia *Analizador, store *Store) error {
	log.Println("Nodo 2: consultando tickets actualizados en los últimos 7 días...")
	ticketsRaw, err := api.ListarTicketsActualizados()
	if err != nil {
		return fmt.Errorf("ticket_list: %w", err)
	}
	_, _, err = procesarTickets(cfg, api, ia, store, ticketsRaw)
	return err
}

func procesarRango(cfg *Config, api *UbersmithClient, ia *Analizador, store *Store, begin, end time.Time) (int, int, error) {
	ticketsRaw, err := api.ListarTicketsEnRango(begin, end)
	if err != nil {
		return 0, 0, fmt.Errorf("ticket_list: %w", err)
	}
	return procesarTickets(cfg, api, ia, store, ticketsRaw)
}

func procesarTickets(cfg *Config, api *UbersmithClient, ia *Analizador, store *Store, ticketsRaw map[string]TicketItem) (int, int, error) {
	log.Printf("API devolvió %d tickets", len(ticketsRaw))

	tickets := filtrarTicketsValidos(ticketsRaw)
	log.Printf("Nodo 3: %d tickets válidos (Solicitud Nueva + Residencial, sin test/FX)", len(tickets))
	if len(tickets) == 0 {
		log.Println("No hay tickets para procesar")
		return 0, 0, nil
	}

	jobs := make(chan TicketItem, len(tickets))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, fail := 0, 0

	workers := cfg.Workers
	if workers > len(tickets) {
		workers = len(tickets)
	}
	log.Printf("Nodo 4: procesando con %d workers", workers)

	for w := 1; w <= workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for ticket := range jobs {
				if err := procesarTicket(id, api, ia, store, ticket); err != nil {
					log.Printf("[worker %d] ticket %s: %v", id, ticket.TicketID.String(), err)
					mu.Lock()
					fail++
					mu.Unlock()
					continue
				}
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(w)
	}

	for _, t := range tickets {
		jobs <- t
	}
	close(jobs)
	wg.Wait()

	log.Printf("Flujo completado. OK=%d errores=%d", ok, fail)
	return ok, fail, nil
}

func procesarTicket(workerID int, api *UbersmithClient, ia *Analizador, store *Store, item TicketItem) error {
	ticketID := item.TicketID.String()
	log.Printf("[worker %d] ticket %s: nodo 5+6 (get + posts)", workerID, ticketID)

	meta, err := api.ObtenerTicket(ticketID)
	if err != nil {
		return fmt.Errorf("ticket_get: %w", err)
	}
	posts, err := api.ListarPosts(ticketID)
	if err != nil {
		return fmt.Errorf("ticket_post_list: %w", err)
	}

	normalizado := normalizarTicket(meta, posts)

	log.Printf("[worker %d] ticket %s: nodo 8 (IA)", workerID, ticketID)
	analizado, err := ia.Analizar(normalizado)
	if err != nil {
		return fmt.Errorf("ia: %w", err)
	}
	analizado.TicketID = ticketID

	if err := store.UpsertTicket(analizado); err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	log.Printf("[worker %d] ticket %s guardado (%s / %s)", workerID, ticketID, analizado.Estatus, analizado.Pueblo)
	return nil
}
