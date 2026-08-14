package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

const promptSistema = `Eres un experto analista de tickets de telecomunicaciones. Tu tarea es analizar los datos iniciales y el historial de mensajes de un ticket para extraer, corregir y devolver información en un formato JSON estricto.

### REGLAS DE EXTRACCIÓN:

1. "Ticket ID":

2. "Tipo de Cliente":

3. "Subject":

4. "Pueblo":
Revisa el valor actual proporcionado. Si el valor es "OTHER" o está vacío, busca en el 'subject' o en el 'body' del historial el municipio correcto. DEBES elegir SOLO de esta lista exacta:
[Adjuntas, Aguada, Aguadilla, Aguas Buenas, Aibonito, Arecibo, Arroyo, Añasco, Barceloneta, Barranquitas, Bayamón, Cabo Rojo, Caguas, Camuy, Canóvanas, Carolina, Cataño, Cayey, Ceiba, Ciales, Cidra, Coamo, Comerío, Corozal, Culebra, Dorado, Fajardo, Florida, Guayama, Guayanilla, Guaynabo, Gurabo, Guánica, Hatillo, Hormigueros, Humacao, Isabela, Jayuya, Juana Díaz, Juncos, Lajas, Lares, Las Marías, Las Piedras, Loiza, Luquillo, Manatí, Maricao, Maunabo, Mayagüez, Moca, Morovis, Naguabo, Naranjito, Orocovis, Patillas, Peñuelas, Ponce, Quebradillas, Rincón, Rio Grande, Sabana Grande, Salinas, San Germán, San Juan, San Lorenzo, San Sebastián, Santa Isabel, Toa Alta, Toa Baja, Trujillo Alto, Utuado, Vega Alta, Vega Baja, Vieques, Villalba, Yabucoa, Yauco].

5. "Fecha y Hora":

6. "Referred by":
Revisa el valor actual. Si es "Otros", "OTHER" o está vacío, busca en el historial de dónde provino el referido. DEBES mapearlo SOLO a una de estas opciones exactas:
"Un amigo", "Otra Compañía", "Guagua", "Oficina", "Radio", "Anuncio TV", "Flyer", "Vecino", "Familiar", "Otro Cliente", "Google / WEB", "Social Media", "Es Cliente", "Fué Cliente". según el contexto.

7. "Estatus":
Lee cuidadosamente todo el historial para determinar el estatus real de la solicitud. SOLO puedes responder con una de estas tres opciones:
- "Instalada": Si el historial indica que el servicio ya fue instalado, activado o completado con éxito.
- "No Instalada": Si el cliente canceló, no tiene cobertura, no procedió, es cobertura negativa o la solicitud fue cerrada sin éxito.
- "En espera": Si la solicitud aún se está trabajando, hay citas pendientes, se dejó mensaje, o está coordinada para una fecha futura.

8. "Motivo (No Instalada)":
SÓLO llena este campo si el "Estatus" es "No Instalada". Escribe la razón principal, estos motivos pueden ser estrictamente: "Quiere es fibra óptica", "Quería Internet con TV", "Tiene deuda pendiente", "Tiene contrato con otra compañía", "Se va de viaje", "Se va a mudar", "Se fue con otra compañía", "No tiene el dinero", "No puede instalar antena", "No LOSS (Residencial Público)", "No le da la velocidad que Quiere", "No Interesado", "No envió documentos", "No contesta", "Mucha Espera", "Es Menor de Edad", "Le pareció Caro", "Cobertura Negativa", "Cliente quedó en llamar", "Canceló la visita", "Canceló la Solicitud", "no indica"
Si el estatus es otro, deja este campo vacío ("").

9. "Plan Instalado":
SÓLO llena este campo si el "Estatus" es "Instalada". Busca en el historial qué plan de velocidad se contrató finalmente, anotando solo la velocidad de subida, estos planes pueden ser estrictamente: "25mb", "50Mb", "100Mb", "200Mb".
Si el estatus es otro, deja este campo vacío ("").

10. "Agente":
Identifica a la primera persona que escribió en el historial (el autor del mensaje más antiguo, es decir, el primer objeto en la lista del historial). Extrae solo el nombre, ignorando el correo (Ej. de "Milagros Rodriguez <mrodriguez...>" extrae "Milagros Rodriguez").

### FORMATO DE SALIDA:
Devuelve ÚNICAMENTE un objeto JSON válido, sin formato markdown ni texto adicional. Las llaves deben ser exactamente estas:
{
  "Ticket ID": "",
  "Tipo de Cliente": "",
  "Subject": "",
  "Pueblo": "",
  "Fecha y Hora": "",
  "Referred by": "",
  "Estatus": "",
  "Motivo (No Instalada)": "",
  "Plan Instalado": "",
  "Agente": ""
}`

type Analizador struct {
	client *openai.Client
	model  string
}

func NewAnalizador(cfg *Config) *Analizador {
	return &Analizador{
		client: openai.NewClient(cfg.OpenAIKey),
		model:  cfg.OpenAIModel,
	}
}

func (a *Analizador) Analizar(ticket TicketNormalizado) (TicketNormalizado, error) {
	historialJSON, err := json.MarshalIndent(ticket.HistorialLimpio, "", "  ")
	if err != nil {
		return TicketNormalizado{}, fmt.Errorf("serializar historial: %w", err)
	}

	userPrompt := fmt.Sprintf(`Por favor, analiza este ticket:

Datos Actuales:

Ticket ID: %s
Tipo de Cliente actual: %s
Subject actual: %s
Pueblo actual: %s
Referred by actual: %s
Fecha y Hora: %s

Historial completo a analizar:

%s`, ticket.TicketID, ticket.TipoCliente, ticket.Subject, ticket.Pueblo, ticket.ReferredBy, ticket.FechaHora, string(historialJSON))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: a.model,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: promptSistema},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
	})
	if err != nil {
		return TicketNormalizado{}, err
	}
	if len(resp.Choices) == 0 {
		return TicketNormalizado{}, fmt.Errorf("la IA no devolvió choices")
	}

	resultado, err := parsearRespuestaIA(resp.Choices[0].Message.Content, ticket.TicketID)
	if err != nil {
		return TicketNormalizado{}, err
	}
	return resultado, nil
}

func parsearRespuestaIA(rawText, ticketIDOriginal string) (TicketNormalizado, error) {
	rawText = strings.TrimSpace(rawText)
	rawText = strings.ReplaceAll(rawText, "```json", "")
	rawText = strings.ReplaceAll(rawText, "```JSON", "")
	rawText = strings.ReplaceAll(rawText, "```", "")
	rawText = strings.TrimSpace(rawText)

	var m map[string]any
	if err := json.Unmarshal([]byte(rawText), &m); err != nil {
		return TicketNormalizado{}, fmt.Errorf("JSON inválido de la IA: %w; raw=%s", err, recortar(rawText, 400))
	}

	id := valorTexto(m, "Ticket ID")
	if id == "" {
		id = ticketIDOriginal
	}

	return TicketNormalizado{
		TicketID:      id,
		TipoCliente:   valorTexto(m, "Tipo de Cliente"),
		Subject:       valorTexto(m, "Subject"),
		Pueblo:        valorTexto(m, "Pueblo"),
		FechaHora:     valorTexto(m, "Fecha y Hora"),
		ReferredBy:    valorTexto(m, "Referred by"),
		Estatus:       valorTexto(m, "Estatus"),
		MotivoNoInst:  valorTexto(m, "Motivo (No Instalada)"),
		PlanInstalado: valorTexto(m, "Plan Instalado"),
		Agente:        valorTexto(m, "Agente"),
	}, nil
}

func valorTexto(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(t))
		}
		return strings.TrimSpace(string(b))
	}
}
