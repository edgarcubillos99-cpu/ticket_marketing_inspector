package main

import "time"

// MetricaRedSocial replica el reporte orgánico de redes (una fila por plataforma + mes).
type MetricaRedSocial struct {
	Plataforma         string    `json:"plataforma"`
	Mes                time.Time `json:"mes"` // siempre día 1 del mes
	Alcance            int64     `json:"alcance"`
	LikesReacciones    int64     `json:"likes_reacciones"`
	Comentarios        int64     `json:"comentarios"`
	Compartir          int64     `json:"compartir"`
	TotalInteracciones int64     `json:"total_interacciones"`
	SeguidoresNetos    int64     `json:"seguidores_netos"`
	TotalSeguidores    int64     `json:"total_seguidores"`
}

// MetricaAnuncio replica el reporte de ads (plataforma + tipo cliente + mes + tipo resultado).
type MetricaAnuncio struct {
	Plataforma    string    `json:"plataforma"`
	TipoCliente   string    `json:"tipo_cliente"` // Residencial | Comercial
	Mes           time.Time `json:"mes"`
	TipoResultado string    `json:"tipo_resultado"` // Clics | Mensajes | Interacciones
	Resultado     int64     `json:"resultado"`
	Inversion     float64   `json:"inversion"`
}

const (
	PlataformaFacebook  = "Facebook"
	PlataformaInstagram = "Instagram"
	PlataformaLinkedIn  = "LinkedIn"
	PlataformaGoogle    = "Google"

	TipoClienteResidencial = "Residencial"
	TipoClienteComercial   = "Comercial"

	TipoResultadoClics          = "Clics"
	TipoResultadoMensajes       = "Mensajes"
	TipoResultadoInteracciones  = "Interacciones"
)

func primerDiaMes(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
}

func mesesEnRango(desde, hasta time.Time, loc *time.Location) []time.Time {
	desde = primerDiaMes(desde, loc)
	hasta = primerDiaMes(hasta, loc)
	if hasta.Before(desde) {
		return nil
	}
	var out []time.Time
	for cur := desde; !cur.After(hasta); cur = cur.AddDate(0, 1, 0) {
		out = append(out, cur)
	}
	return out
}
