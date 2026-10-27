package marketing

import (
	"agente_marketing/internal/config"
	"agente_marketing/internal/domain"
	"agente_marketing/internal/store"
	"fmt"
	"log"
	"time"
)

// ejecutarFlujoMarketing sincroniza redes sociales orgánicas + anuncios en el mismo ciclo semanal.
func EjecutarFlujo(cfg *config.Config, store *store.Store) error {
	if !cfg.MarketingEnabled() {
		log.Println("Marketing (redes/ads): omitido — faltan credenciales en .env (ver docs/INSTRUCCIONES_REDES_Y_ANUNCIOS.txt)")
		return nil
	}

	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		return fmt.Errorf("zona horaria %s: %w", cfg.CronTZ, err)
	}

	hasta := domain.PrimerDiaMes(time.Now().In(loc), loc)
	desde := hasta.AddDate(0, -(cfg.MarketingMonthsLookback - 1), 0)
	log.Printf("Marketing: sincronizando %s → %s (%d meses)",
		desde.Format("2006-01"), hasta.Format("2006-01"), cfg.MarketingMonthsLookback)

	return sincronizarMarketingRango(cfg, store, desde, hasta, loc)
}

func EjecutarBackfill(cfg *config.Config, store *store.Store) error {
	if !cfg.MarketingEnabled() {
		log.Println("Marketing backfill: omitido — faltan credenciales")
		return nil
	}

	loc, err := time.LoadLocation(cfg.CronTZ)
	if err != nil {
		return fmt.Errorf("zona horaria %s: %w", cfg.CronTZ, err)
	}

	desde, err := time.ParseInLocation("2006-01-02", cfg.BackfillFrom, loc)
	if err != nil {
		return fmt.Errorf("BACKFILL_FROM %q: usa YYYY-MM-DD", cfg.BackfillFrom)
	}
	desde = domain.PrimerDiaMes(desde, loc)
	hasta := domain.PrimerDiaMes(time.Now().In(loc), loc)

	log.Printf("Marketing backfill: %s → %s", desde.Format("2006-01"), hasta.Format("2006-01"))
	return sincronizarMarketingRango(cfg, store, desde, hasta, loc)
}

func sincronizarMarketingRango(cfg *config.Config, store *store.Store, desde, hasta time.Time, loc *time.Location) error {
	var socialOK, socialFail, adsOK, adsFail int

	if cfg.MetaOrganicoEnabled() || cfg.MetaAdsEnabled() {
		meta := NewMetaClient(cfg)
		if cfg.MetaOrganicoEnabled() {
			rows, err := meta.FetchOrganico(desde, hasta, loc)
			if err != nil {
				log.Printf("meta organico: %v", err)
				socialFail++
			}
			if len(rows) > 0 {
				ok, fail := guardarSocial(store, rows)
				socialOK += ok
				socialFail += fail
			}
		}
		if cfg.MetaAdsEnabled() {
			rows, err := meta.FetchAds(desde, hasta, loc)
			if err != nil {
				log.Printf("meta ads: %v", err)
				adsFail++
			} else {
				ok, fail := guardarAds(store, rows)
				adsOK += ok
				adsFail += fail
			}
		}
	}

	if cfg.LinkedInOrganicoEnabled() || cfg.LinkedInAdsEnabled() {
		li := NewLinkedInClient(cfg)
		if cfg.LinkedInOrganicoEnabled() {
			rows, err := li.FetchOrganico(desde, hasta, loc)
			if err != nil {
				log.Printf("linkedin organico: %v", err)
				socialFail++
			} else {
				ok, fail := guardarSocial(store, rows)
				socialOK += ok
				socialFail += fail
			}
		}
		if cfg.LinkedInAdsEnabled() {
			rows, err := li.FetchAds(desde, hasta, loc)
			if err != nil {
				log.Printf("linkedin ads: %v", err)
				adsFail++
			} else {
				ok, fail := guardarAds(store, rows)
				adsOK += ok
				adsFail += fail
			}
		}
	}

	if cfg.GoogleAdsEnabled() {
		gads := NewGoogleAdsClient(cfg)
		rows, err := gads.FetchAds(desde, hasta, loc)
		if err != nil {
			log.Printf("google ads: %v", err)
			adsFail++
		} else {
			ok, fail := guardarAds(store, rows)
			adsOK += ok
			adsFail += fail
		}
	}

	var gmbOK, gmbFail, emailsOK, emailsFail int
	if cfg.GMBEnabled() {
		gmb := NewGMBClient(cfg)
		rows, err := gmb.Fetch(desde, hasta, loc)
		if err != nil {
			log.Printf("mybusiness: %v", err)
			gmbFail++
		} else {
			ok, fail := guardarGMB(store, rows)
			gmbOK += ok
			gmbFail += fail
		}
	}

	if cfg.EmailsEnabled() {
		client, err := NewEmailsSource(cfg)
		if err != nil {
			log.Printf("emails config: %v", err)
			emailsFail++
		} else {
			rows, err := client.Fetch(desde, hasta, loc)
			if err != nil {
				log.Printf("emails: %v", err)
				emailsFail++
			} else {
				ok, fail := guardarEmails(store, rows)
				emailsOK += ok
				emailsFail += fail
			}
		}
	}

	log.Printf("Marketing completado. redes OK=%d err=%d | ads OK=%d err=%d | gmb OK=%d err=%d | emails OK=%d err=%d",
		socialOK, socialFail, adsOK, adsFail, gmbOK, gmbFail, emailsOK, emailsFail)
	return nil
}

func guardarSocial(store *store.Store, rows []domain.MetricaRedSocial) (ok, fail int) {
	for _, m := range rows {
		if err := store.UpsertMetricaSocial(m); err != nil {
			log.Printf("guardar social %s %s: %v", m.Plataforma, m.Mes.Format("2006-01"), err)
			fail++
			continue
		}
		log.Printf("social guardado: %s %s alcance=%d interacciones=%d seguidores=%d",
			m.Plataforma, m.Mes.Format("2006-01"), m.Alcance, m.TotalInteracciones, m.TotalSeguidores)
		ok++
	}
	return ok, fail
}

func guardarAds(store *store.Store, rows []domain.MetricaAnuncio) (ok, fail int) {
	for _, m := range rows {
		if err := store.UpsertMetricaAnuncio(m); err != nil {
			log.Printf("guardar ads %s %s %s: %v", m.Plataforma, m.TipoCliente, m.Mes.Format("2006-01"), err)
			fail++
			continue
		}
		log.Printf("ads guardado: %s %s %s %s=%d inversion=%.2f",
			m.Plataforma, m.TipoCliente, m.Mes.Format("2006-01"), m.TipoResultado, m.Resultado, m.Inversion)
		ok++
	}
	return ok, fail
}

func guardarGMB(store *store.Store, rows []domain.MetricaMyBusiness) (ok, fail int) {
	for _, m := range rows {
		if err := store.UpsertMetricaMyBusiness(m); err != nil {
			log.Printf("guardar gmb %s %s: %v", m.Sede, m.Mes.Format("2006-01"), err)
			fail++
			continue
		}
		log.Printf("gmb guardado: %s %s vistas=%d llamadas=%d web=%d interacciones=%d",
			m.Sede, m.Mes.Format("2006-01"), m.Vistas, m.Llamadas, m.IrAlSitioWeb, m.InteraccionesTotales)
		ok++
	}
	return ok, fail
}

func guardarEmails(store *store.Store, rows []domain.MetricaEmail) (ok, fail int) {
	for _, m := range rows {
		if err := store.UpsertMetricaEmail(m); err != nil {
			log.Printf("guardar email %s %s %s: %v", m.Tipo, m.TipoCliente, m.Mes.Format("2006-01"), err)
			fail++
			continue
		}
		log.Printf("email guardado: %s %s %s cantidad=%d",
			m.Tipo, m.TipoCliente, m.Mes.Format("2006-01"), m.Cantidad)
		ok++
	}
	return ok, fail
}
