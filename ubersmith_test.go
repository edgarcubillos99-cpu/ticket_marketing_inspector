package main

import (
	"testing"
	"time"
)

func TestPeriodosMensuales(t *testing.T) {
	loc, err := time.LoadLocation("America/Puerto_Rico")
	if err != nil {
		t.Fatal(err)
	}

	desde := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	hasta := time.Date(2024, 3, 15, 12, 0, 0, 0, loc)
	got := periodosMensuales(desde, hasta, loc)
	if len(got) != 3 {
		t.Fatalf("esperaba 3 periodos, obtuve %d", len(got))
	}

	if !got[0][0].Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, loc)) ||
		!got[0][1].Equal(time.Date(2024, 2, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("enero inesperado: %v → %v", got[0][0], got[0][1])
	}
	if !got[1][0].Equal(time.Date(2024, 2, 1, 0, 0, 0, 0, loc)) ||
		!got[1][1].Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("febrero inesperado: %v → %v", got[1][0], got[1][1])
	}
	if !got[2][0].Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, loc)) ||
		!got[2][1].Equal(hasta) {
		t.Fatalf("marzo inesperado: %v → %v", got[2][0], got[2][1])
	}
}
