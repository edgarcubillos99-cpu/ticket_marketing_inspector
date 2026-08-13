package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	reHTML     = regexp.MustCompile(`(?i)</?[^>]+>`)
	reEspacios = regexp.MustCompile(`\s+`)
)

func filtrarTicketsValidos(tickets map[string]TicketItem) []TicketItem {
	validos := make([]TicketItem, 0, len(tickets))
	for _, ticket := range tickets {
		subject := strings.ToLower(ticket.Subject.String())
		contieneSolicitud := strings.Contains(subject, "solicitud nueva")
		esResidencial := ticket.Metadata != nil && ticket.Metadata.SLA.String() == "Residencial"
		esTest := strings.Contains(subject, "solicitud de nuevo servicio test automatico") ||
			strings.Contains(subject, "solicitud nueva test automatico")

		if contieneSolicitud && esResidencial && !esTest {
			validos = append(validos, ticket)
		}
	}

	sort.Slice(validos, func(i, j int) bool {
		ti, _ := strconv.ParseInt(validos[i].Timestamp.String(), 10, 64)
		tj, _ := strconv.ParseInt(validos[j].Timestamp.String(), 10, 64)
		return ti > tj
	})
	return validos
}

func limpiarHTML(htmlStr string) string {
	if htmlStr == "" {
		return ""
	}
	sinTags := reHTML.ReplaceAllString(htmlStr, " ")
	return strings.TrimSpace(reEspacios.ReplaceAllString(sinTags, " "))
}

func formatearFechaPR(timestampStr string) string {
	ts, err := strconv.ParseInt(strings.TrimSpace(timestampStr), 10, 64)
	if err != nil || ts == 0 {
		return ""
	}

	loc, err := time.LoadLocation("America/Puerto_Rico")
	if err != nil {
		loc = time.UTC
	}
	return time.Unix(ts, 0).In(loc).Format("2006-01-02 15:04:05")
}

func limpiarNombreAgente(nombre string) string {
	nombre = strings.TrimSpace(nombre)
	if i := strings.Index(nombre, "<"); i >= 0 {
		nombre = strings.TrimSpace(nombre[:i])
	}
	return nombre
}

// normalizarTicket replica el nodo 7: une ticket_get + ticket_post_list.
func normalizarTicket(meta *TicketItem, posts []TicketPost) TicketNormalizado {
	sort.Slice(posts, func(i, j int) bool {
		ti, _ := strconv.ParseInt(posts[i].Timestamp.String(), 10, 64)
		tj, _ := strconv.ParseInt(posts[j].Timestamp.String(), 10, 64)
		return ti < tj
	})

	historial := make([]PostLimpio, 0, len(posts))
	for _, post := range posts {
		historial = append(historial, PostLimpio{
			Autor:     post.Author.String(),
			TicketID:  post.TicketID.String(),
			Timestamp: post.Timestamp.String(),
			Body:      limpiarHTML(post.Body.String()),
			Subject:   post.Subject.String(),
		})
	}

	tipoCliente := "Residencial"
	pueblo := ""
	referredBy := ""
	motivo := ""
	if meta.Metadata != nil {
		if sla := meta.Metadata.SLA.String(); sla != "" {
			tipoCliente = sla
		}
		pueblo = meta.Metadata.Municipio.String()
		referredBy = meta.Metadata.ReferredBy.String()
		motivo = meta.Metadata.ResolutionType.String()
	}

	return TicketNormalizado{
		TicketID:        meta.TicketID.String(),
		TipoCliente:     tipoCliente,
		Subject:         meta.Subject.String(),
		Pueblo:          pueblo,
		FechaHora:       formatearFechaPR(meta.Timestamp.String()),
		ReferredBy:      referredBy,
		Estatus:         meta.TypeName.String(),
		MotivoNoInst:    motivo,
		Agente:          limpiarNombreAgente(meta.AdminName.String()),
		HistorialLimpio: historial,
	}
}
