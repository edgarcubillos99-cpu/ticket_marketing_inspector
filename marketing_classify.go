package main

import (
	"regexp"
	"strings"
)

type clasificadorCliente struct {
	residencial *regexp.Regexp
	comercial   *regexp.Regexp
}

func newClasificadorCliente(resPattern, comPattern string) *clasificadorCliente {
	c := &clasificadorCliente{}
	if p, err := regexp.Compile(resPattern); err == nil {
		c.residencial = p
	}
	if p, err := regexp.Compile(comPattern); err == nil {
		c.comercial = p
	}
	return c
}

// ClasificarTipoCliente infiere Residencial/Comercial del nombre de campaña.
// Si no hay match, retorna "" (la fila se omite o se trata aparte).
func (c *clasificadorCliente) ClasificarTipoCliente(nombre string) string {
	nombre = strings.TrimSpace(nombre)
	if nombre == "" {
		return ""
	}
	if c.residencial != nil && c.residencial.MatchString(nombre) {
		return TipoClienteResidencial
	}
	if c.comercial != nil && c.comercial.MatchString(nombre) {
		return TipoClienteComercial
	}
	return ""
}

func sumInteracciones(likes, comentarios, compartir int64) int64 {
	return likes + comentarios + compartir
}
