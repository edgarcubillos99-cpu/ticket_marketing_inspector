package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type Store struct {
	db    *sql.DB
	table string
}

func NewStore(cfg *Config) (*Store, error) {
	mc := mysql.NewConfig()
	mc.User = cfg.MySQLUser
	mc.Passwd = cfg.MySQLPassword
	mc.Net = "tcp"
	mc.Addr = net.JoinHostPort(cfg.MySQLHost, cfg.MySQLPort)
	mc.DBName = cfg.MySQLDatabase
	mc.ParseTime = true
	mc.Params = map[string]string{
		"charset": "utf8mb4",
		"loc":     "America/Puerto_Rico",
	}
	if cfg.MySQLTLS != "" {
		mc.TLSConfig = cfg.MySQLTLS
	}

	db, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("abrir MySQL: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("conectar MySQL: %w", err)
	}

	store := &Store{db: db, table: sanitizeTableName(cfg.MySQLTable)}
	if err := store.ensureTable(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) ensureTable() error {
	exists, err := s.tableExists()
	if err != nil {
		return err
	}
	if exists {
		log.Printf("MySQL: tabla %s encontrada", s.table)
		return nil
	}

	log.Printf("MySQL: tabla %s no existe, creándola...", s.table)
	q := fmt.Sprintf(`
CREATE TABLE %s (
  ticket_id INT NOT NULL,
  tipo_cliente VARCHAR(100) DEFAULT NULL,
  asunto TEXT,
  pueblo VARCHAR(100) DEFAULT NULL,
  fecha_hora DATETIME DEFAULT NULL,
  referred_by VARCHAR(100) DEFAULT NULL,
  estatus VARCHAR(50) DEFAULT NULL,
  motivo VARCHAR(255) DEFAULT NULL,
  plan_instalado VARCHAR(100) DEFAULT NULL,
  agente VARCHAR(100) DEFAULT NULL,
  PRIMARY KEY (ticket_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, s.table)

	if _, err := s.db.Exec(q); err != nil {
		return fmt.Errorf("crear tabla %s: %w", s.table, err)
	}
	log.Printf("MySQL: tabla %s creada", s.table)
	return nil
}

func (s *Store) tableExists() (bool, error) {
	var name string
	err := s.db.QueryRow(`
SELECT TABLE_NAME
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
LIMIT 1`, s.table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verificar tabla %s: %w", s.table, err)
	}
	return true, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) UpsertTicket(t TicketNormalizado) error {
	ticketID, err := strconv.ParseInt(strings.TrimSpace(t.TicketID), 10, 64)
	if err != nil || ticketID == 0 {
		return fmt.Errorf("ticket_id inválido %q", t.TicketID)
	}

	q := fmt.Sprintf(`
INSERT INTO %s (
  ticket_id, tipo_cliente, asunto, pueblo, fecha_hora,
  referred_by, estatus, motivo, plan_instalado, agente
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  tipo_cliente = VALUES(tipo_cliente),
  asunto = VALUES(asunto),
  pueblo = VALUES(pueblo),
  fecha_hora = VALUES(fecha_hora),
  referred_by = VALUES(referred_by),
  estatus = VALUES(estatus),
  motivo = VALUES(motivo),
  plan_instalado = VALUES(plan_instalado),
  agente = VALUES(agente)`, s.table)

	_, err = s.db.Exec(q,
		ticketID,
		nullSiVacio(recortarRunes(t.TipoCliente, 100)),
		nullSiVacio(t.Subject),
		nullSiVacio(recortarRunes(t.Pueblo, 100)),
		nullSiVacio(strings.TrimSpace(t.FechaHora)),
		nullSiVacio(recortarRunes(t.ReferredBy, 100)),
		nullSiVacio(recortarRunes(t.Estatus, 50)),
		nullSiVacio(recortarRunes(t.MotivoNoInst, 255)),
		nullSiVacio(recortarRunes(t.PlanInstalado, 100)),
		nullSiVacio(recortarRunes(t.Agente, 100)),
	)
	if err != nil {
		return fmt.Errorf("upsert ticket %d: %w", ticketID, err)
	}
	return nil
}

func nullSiVacio(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func recortarRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func sanitizeTableName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tickets_osnet"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "tickets_osnet"
	}
	return b.String()
}
