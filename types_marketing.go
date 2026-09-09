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
	TipoResultado string    `json:"tipo_resultado"` // Clics | Mensajes | Interacciones | Leads | Alcance | ThruPlays
	Resultado     int64     `json:"resultado"`
	Inversion     float64   `json:"inversion"`
}

// MetricaMyBusiness replica el reporte de Google Business Profile por sede y mes.
type MetricaMyBusiness struct {
	Sede                 string    `json:"sede"`
	Mes                  time.Time `json:"mes"`
	Vistas               int64     `json:"vistas"`
	Mensajes             int64     `json:"mensajes"`
	Llamadas             int64     `json:"llamadas"`
	ComoLlegar           int64     `json:"como_llegar"`
	IrAlSitioWeb         int64     `json:"ir_al_sitio_web"`
	InteraccionesTotales int64     `json:"interacciones_totales"`
}

// MetricaEmail replica el recuento mensual de formularios/emails del plugin web.
type MetricaEmail struct {
	Tipo        string    `json:"tipo"`
	TipoCliente string    `json:"tipo_cliente"`
	Mes         time.Time `json:"mes"`
	Cantidad    int64     `json:"cantidad"`
}

const (
	PlataformaFacebook  = "Facebook"
	PlataformaInstagram = "Instagram"
	PlataformaLinkedIn  = "LinkedIn"
	PlataformaGoogle    = "Google"

	TipoClienteResidencial = "Residencial"
	TipoClienteComercial   = "Comercial"

	TipoResultadoClics         = "Clics"
	TipoResultadoMensajes      = "Mensajes"
	TipoResultadoInteracciones = "Interacciones"
	TipoResultadoLeads         = "Leads"
	TipoResultadoAlcance       = "Alcance"
	TipoResultadoThruPlays     = "ThruPlays"

	TipoEmailCobertura             = "Cobertura"
	TipoEmailInternetResidencial   = "Internet Residencial"
	TipoEmailTelefoniaResidencial  = "Telefonía Residencial"
	TipoEmailInternetComercial     = "Internet Comercial"
	TipoEmailTelefoniaComercial    = "Telefonía Comercial"
	TipoEmailOtros                 = "Otros"
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
