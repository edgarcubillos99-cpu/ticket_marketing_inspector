package main

import "testing"

func TestClasificarTipoCliente(t *testing.T) {
	c := newClasificadorCliente(
		`(?i)residenc|residential|\bres\b`,
		`(?i)comercial|commercial|business|\bcom\b|\bb2b\b`,
	)

	cases := []struct {
		nombre string
		want   string
	}{
		{"Google Residencial Search", TipoClienteResidencial},
		{"FB Res - Mensajes", TipoClienteResidencial},
		{"Google Comercial Brand", TipoClienteComercial},
		{"LinkedIn B2B Leads", TipoClienteComercial},
		{"Campaña genérica", ""},
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
