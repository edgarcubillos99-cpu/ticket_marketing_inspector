package main

import "testing"

func TestClasificarTipoCliente(t *testing.T) {
	c := newClasificadorCliente(
		`(?i)residenc|residential|\bres\b|hogar|instalaci|router|back\s*to\s*school|bts|b[uú]squeda|clientes potenciales|leads|acp|promo|v[ií]deo|display|anuncio`,
		`(?i)comercial|commercial|business|\bb2b\b|small\s*business`,
	)

	cases := []struct {
		nombre string
		want   string
	}{
		{"Google Residencial Search", TipoClienteResidencial},
		{"FB Res - Mensajes", TipoClienteResidencial},
		{"Busqueda Hogar Osnet 2026", TipoClienteResidencial},
		{"Display Residencial Osnet 2024 agosto", TipoClienteResidencial},
		{"Back To School-Display agosto-2026#2 #2", TipoClienteResidencial},
		{"Tráfico Small Business Osnet 2023", TipoClienteComercial},
		{"Google Comercial Brand", TipoClienteComercial},
		{"LinkedIn B2B Leads", TipoClienteComercial},
		{"Campaña genérica sin pistas", ""},
	}

	for _, tc := range cases {
		got := c.ClasificarTipoCliente(tc.nombre)
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.nombre, got, tc.want)
		}
	}
}

func TestSumInteracciones(t *testing.T) {
	if got := sumInteracciones(10, 5, 2); got != 17 {
		t.Fatalf("got %d", got)
	}
}

func TestMetaKPIsFacebook(t *testing.T) {
	actions := []metaAction{
		{ActionType: "lead", Value: "12"},
		{ActionType: "onsite_conversion.lead_grouped", Value: "10"},
		{ActionType: "onsite_conversion.messaging_conversation_started_7d", Value: "5"},
		{ActionType: "post_engagement", Value: "100"},
	}
	thruplay := []metaAction{{ActionType: "video_view", Value: "80"}}

	kpis := metaKPIs(PlataformaFacebook, "50", "900", actions, thruplay)
	got := map[string]int64{}
	for _, k := range kpis {
		got[k.Tipo] = k.Valor
	}
	if got[TipoResultadoLeads] != 12 {
		t.Fatalf("leads=%d want 12 (max)", got[TipoResultadoLeads])
	}
	if got[TipoResultadoMensajes] != 5 {
		t.Fatalf("mensajes=%d", got[TipoResultadoMensajes])
	}
	if got[TipoResultadoAlcance] != 900 {
		t.Fatalf("alcance=%d", got[TipoResultadoAlcance])
	}
	if got[TipoResultadoThruPlays] != 80 {
		t.Fatalf("thruplays=%d", got[TipoResultadoThruPlays])
	}
	if _, ok := got[TipoResultadoInteracciones]; ok {
		t.Fatal("facebook no debe emitir Interacciones")
	}
}

