package tickets

import (
	"fmt"
	"log"
	"sync"
	"time"

	"agente_marketing/internal/config"
	"agente_marketing/internal/domain"
	"agente_marketing/internal/store"
)

func EjecutarBackfill(cfg *config.Config, api *UbersmithClient, ia *Analizador, db *store.Store) error {
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
		ok, fail, err := procesarRango(cfg, api, ia, db, p[0], p[1])
		if err != nil {
			return fmt.Errorf("periodo %s: %w", p[0].Format("2006-01"), err)
		}
		okTotal += ok
		failTotal += fail
	}

	log.Printf("Backfill completado. OK=%d errores=%d", okTotal, failTotal)
	return nil
}

func EjecutarFlujo(cfg *config.Config, api *UbersmithClient, ia *Analizador, db *store.Store) error {
	log.Println("Nodo 2: consultando tickets actualizados en los últimos 7 días...")
	ticketsRaw, err := api.ListarTicketsActualizados()
	if err != nil {
		return fmt.Errorf("ticket_list: %w", err)
	}
	_, _, err = procesarTickets(cfg, api, ia, db, ticketsRaw)
	return err
}

func procesarRango(cfg *config.Config, api *UbersmithClient, ia *Analizador, db *store.Store, begin, end time.Time) (int, int, error) {
	ticketsRaw, err := api.ListarTicketsEnRango(begin, end)
	if err != nil {
		return 0, 0, fmt.Errorf("ticket_list: %w", err)
	}
	return procesarTickets(cfg, api, ia, db, ticketsRaw)
}

func procesarTickets(cfg *config.Config, api *UbersmithClient, ia *Analizador, db *store.Store, ticketsRaw map[string]domain.TicketItem) (int, int, error) {
	log.Printf("API devolvió %d tickets", len(ticketsRaw))

	items := filtrarTicketsValidos(ticketsRaw)
	log.Printf("Nodo 3: %d tickets válidos (Solicitud Nueva + Residencial, sin test/FX)", len(items))
	if len(items) == 0 {
		log.Println("No hay tickets para procesar")
		return 0, 0, nil
	}

	jobs := make(chan domain.TicketItem, len(items))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, fail := 0, 0

	workers := cfg.Workers
	if workers > len(items) {
		workers = len(items)
	}
	log.Printf("Nodo 4: procesando con %d workers", workers)

	for w := 1; w <= workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for ticket := range jobs {
				if err := procesarTicket(id, api, ia, db, ticket); err != nil {
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

	for _, t := range items {
		jobs <- t
	}
	close(jobs)
	wg.Wait()

	log.Printf("Flujo completado. OK=%d errores=%d", ok, fail)
	return ok, fail, nil
}

func procesarTicket(workerID int, api *UbersmithClient, ia *Analizador, db *store.Store, item domain.TicketItem) error {
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

	if err := db.UpsertTicket(analizado); err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	log.Printf("[worker %d] ticket %s guardado (%s / %s)", workerID, ticketID, analizado.Estatus, analizado.Pueblo)
	return nil
}
