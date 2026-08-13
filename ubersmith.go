package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type UbersmithClient struct {
	baseURL string
	user    string
	token   string
	limit   int
	http    *http.Client
}

func NewUbersmithClient(cfg *Config) *UbersmithClient {
	return &UbersmithClient{
		baseURL: strings.TrimRight(cfg.UbersmithBaseURL, "/") + "/",
		user:    cfg.UbersmithUser,
		token:   cfg.UbersmithToken,
		limit:   cfg.UbersmithLimit,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *UbersmithClient) call(method string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("method", method)

	endpoint := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: leer respuesta: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, recortar(string(body), 300))
	}

	var apiResp struct {
		Status       bool            `json:"status"`
		ErrorCode    any             `json:"error_code"`
		ErrorMessage string          `json:"error_message"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("%s: JSON inválido: %w (%s)", method, err, recortar(string(body), 300))
	}
	if !apiResp.Status {
		return nil, fmt.Errorf("%s: API error (%v): %s", method, apiResp.ErrorCode, apiResp.ErrorMessage)
	}
	return apiResp.Data, nil
}

// ListarTicketsActualizados replica el nodo 2: activity_begin = hace 7 días, internal_ticket=2.
func (c *UbersmithClient) ListarTicketsActualizados() (map[string]TicketItem, error) {
	haceUnaSemana := time.Now().Add(-7 * 24 * time.Hour).Unix()
	params := url.Values{}
	params.Set("activity_begin", strconv.FormatInt(haceUnaSemana, 10))
	params.Set("internal_ticket", "2")
	// Incluye todos los custom fields (sla, municipio, etc.) para el filtro del nodo 3.
	params.Set("metadata", "1")
	if c.limit > 0 {
		params.Set("limit", strconv.Itoa(c.limit))
	}

	raw, err := c.call("support.ticket_list", params)
	if err != nil {
		return nil, err
	}
	return decodeTicketMap(raw)
}

// ObtenerTicket replica el nodo 5: support.ticket_get.
func (c *UbersmithClient) ObtenerTicket(ticketID string) (*TicketItem, error) {
	params := url.Values{}
	params.Set("ticket_id", ticketID)

	raw, err := c.call("support.ticket_get", params)
	if err != nil {
		return nil, err
	}

	ticket, err := unwrapTicket(raw)
	if err != nil {
		return nil, fmt.Errorf("ticket_get %s: %w", ticketID, err)
	}
	return ticket, nil
}

// ListarPosts replica el nodo 6: support.ticket_post_list.
func (c *UbersmithClient) ListarPosts(ticketID string) ([]TicketPost, error) {
	params := url.Values{}
	params.Set("ticket_id", ticketID)

	raw, err := c.call("support.ticket_post_list", params)
	if err != nil {
		return nil, err
	}

	posts, err := decodePostMap(raw)
	if err != nil {
		return nil, fmt.Errorf("ticket_post_list %s: %w", ticketID, err)
	}
	return posts, nil
}

func decodeTicketMap(raw json.RawMessage) (map[string]TicketItem, error) {
	if esDataVacia(raw) {
		return map[string]TicketItem{}, nil
	}
	var tickets map[string]TicketItem
	if err := json.Unmarshal(raw, &tickets); err != nil {
		return nil, fmt.Errorf("ticket_list: data inesperada: %w", err)
	}
	return tickets, nil
}

func unwrapTicket(raw json.RawMessage) (*TicketItem, error) {
	if esDataVacia(raw) {
		return nil, fmt.Errorf("respuesta vacía")
	}

	var directo TicketItem
	if err := json.Unmarshal(raw, &directo); err == nil && directo.TicketID.String() != "" {
		return &directo, nil
	}

	var wrapped map[string]TicketItem
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	for _, t := range wrapped {
		if t.TicketID.String() != "" {
			cp := t
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("no se encontró ticket_id en la respuesta")
}

func decodePostMap(raw json.RawMessage) ([]TicketPost, error) {
	if esDataVacia(raw) {
		return nil, nil
	}
	var postsMap map[string]TicketPost
	if err := json.Unmarshal(raw, &postsMap); err != nil {
		return nil, err
	}
	posts := make([]TicketPost, 0, len(postsMap))
	for _, p := range postsMap {
		posts = append(posts, p)
	}
	return posts, nil
}

func esDataVacia(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "[]" || s == `""`
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
