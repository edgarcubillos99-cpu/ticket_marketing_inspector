package main

import (
	"bytes"
	"encoding/json"
)

// SoftString acepta string, número o null de Ubersmith sin romper el decode.
type SoftString string

func (s SoftString) String() string { return string(s) }

func (s *SoftString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		*s = SoftString(str)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err == nil {
		*s = SoftString(num.String())
		return nil
	}
	*s = ""
	return nil
}

type TicketItem struct {
	TicketID  SoftString      `json:"ticket_id"`
	Subject   SoftString      `json:"subject"`
	Timestamp SoftString      `json:"timestamp"`
	TypeName  SoftString      `json:"type_name"`
	AdminName SoftString      `json:"admin_name"`
	QName     SoftString      `json:"q_name"`
	Metadata  *TicketMetadata `json:"metadata,omitempty"`
}

type TicketMetadata struct {
	SLA            SoftString `json:"sla"`
	Municipio      SoftString `json:"municipio"`
	ReferredBy     SoftString `json:"referred_by"`
	ResolutionType SoftString `json:"resolution_type"`
}

type TicketPost struct {
	Author    SoftString `json:"author"`
	TicketID  SoftString `json:"ticket_id"`
	Timestamp SoftString `json:"timestamp"`
	Body      SoftString `json:"body"`
	Subject   SoftString `json:"subject"`
}

type PostLimpio struct {
	Autor     string `json:"autor"`
	TicketID  string `json:"ticket_id"`
	Timestamp string `json:"timestamp"`
	Body      string `json:"body"`
	Subject   string `json:"subject"`
}

// TicketNormalizado es el payload del nodo 7 (incluye historial) y del nodo 9 (sin historial).
type TicketNormalizado struct {
	TicketID        string       `json:"Ticket ID"`
	TipoCliente     string       `json:"Tipo de Cliente"`
	Subject         string       `json:"Subject"`
	Pueblo          string       `json:"Pueblo"`
	FechaHora       string       `json:"Fecha y Hora"`
	ReferredBy      string       `json:"Referred by"`
	Estatus         string       `json:"Estatus"`
	MotivoNoInst    string       `json:"Motivo (No Instalada)"`
	PlanInstalado   string       `json:"Plan Instalado,omitempty"`
	Agente          string       `json:"Agente"`
	HistorialLimpio []PostLimpio `json:"historialLimpio,omitempty"`
}
