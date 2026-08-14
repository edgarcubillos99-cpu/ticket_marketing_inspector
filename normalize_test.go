package main

import "testing"

func TestFiltrarTicketsValidos(t *testing.T) {
	tickets := map[string]TicketItem{
		"1": {
			TicketID:  "1",
			Subject:   "Solicitud Nueva - Bayamón",
			Timestamp: "100",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
		"2": {
			TicketID:  "2",
			Subject:   "Solicitud Nueva Test Automatico",
			Timestamp: "200",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
		"3": {
			TicketID:  "3",
			Subject:   "Solicitud Nueva Comercial",
			Timestamp: "300",
			Metadata:  &TicketMetadata{SLA: "Comercial"},
		},
		"4": {
			TicketID:  "4",
			Subject:   "Cambio de plan",
			Timestamp: "400",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
		"5": {
			TicketID:  "5",
			Subject:   "solicitud de nuevo servicio test automatico residencial",
			Timestamp: "500",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
		"6": {
			TicketID:  "6",
			Subject:   "SOLICITUD NUEVA Ponce",
			Timestamp: "50",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
		"7": {
			TicketID:  "7",
			Subject:   "Solicitud Nueva - Fibra",
			Timestamp: "600",
			QName:     "Cola FX",
			Metadata:  &TicketMetadata{SLA: "Residencial"},
		},
	}

	got := filtrarTicketsValidos(tickets)
	if len(got) != 2 {
		t.Fatalf("esperaba 2 tickets, obtuve %d", len(got))
	}
	if got[0].TicketID.String() != "1" || got[1].TicketID.String() != "6" {
		t.Fatalf("orden inesperado: %s, %s", got[0].TicketID, got[1].TicketID)
	}
}

func TestParsearRespuestaIA(t *testing.T) {
	raw := "```json\n{\n  \"Ticket ID\": 12345,\n  \"Tipo de Cliente\": \"Residencial\",\n  \"Subject\": \"Solicitud Nueva\",\n  \"Pueblo\": \"Ponce\",\n  \"Fecha y Hora\": \"2026-08-01 13:00:00\",\n  \"Referred by\": \"Social Media\",\n  \"Estatus\": \"Instalada\",\n  \"Motivo (No Instalada)\": \"\",\n  \"Plan Instalado\": \"50 Mb\",\n  \"Agente\": \"Gabriela Silva\"\n}\n```"

	got, err := parsearRespuestaIA(raw, "999")
	if err != nil {
		t.Fatal(err)
	}
	if got.TicketID != "12345" {
		t.Fatalf("Ticket ID=%q", got.TicketID)
	}
	if got.Estatus != "Instalada" || got.PlanInstalado != "50 Mb" || got.Pueblo != "Ponce" {
		t.Fatalf("campos inesperados: %+v", got)
	}
}
