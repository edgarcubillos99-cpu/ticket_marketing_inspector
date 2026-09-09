package main

import (
	"reflect"
	"testing"
)

func TestParseGMBLocationMap(t *testing.T) {
	got := parseGMBLocationMap("Humacao:locations/111; Yauco:222; Bayamón:accounts/9/locations/333")
	want := map[string]string{
		"Humacao": "locations/111",
		"Yauco":   "locations/222",
		"Bayamón": "locations/333",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseEmailsFormMap(t *testing.T) {
	got, err := parseEmailsFormMap("1:Cobertura:Residencial;4:Internet Comercial:Comercial")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].FormID != 1 || got[0].Tipo != "Cobertura" || got[1].TipoCliente != "Comercial" {
		t.Fatalf("unexpected %#v", got)
	}
}

func TestLocationResourceName(t *testing.T) {
	if locationResourceName("accounts/1/locations/99") != "locations/99" {
		t.Fatal(locationResourceName("accounts/1/locations/99"))
	}
}
