// Package store persists users, sessions, settings and seen clients in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadySetUp  = errors.New("an admin account already exists")
	ErrBadCredential = errors.New("invalid username or password")
)

const sessionTTL = 30 * 24 * time.Hour

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("ipxemultiboot-dummy"), bcrypt.DefaultCost)
	return h
})

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS clients (
	mac        TEXT PRIMARY KEY,
	ip         TEXT NOT NULL DEFAULT '',
	firmware   TEXT NOT NULL DEFAULT '',
	last_iso   TEXT NOT NULL DEFAULT '',
	last_event TEXT NOT NULL DEFAULT '',
	first_seen INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	stage      TEXT NOT NULL DEFAULT '',
	attempt_at INTEGER NOT NULL DEFAULT 0,
	last_ipxe  INTEGER NOT NULL DEFAULT 0
);
`

// migrations add columns to databases created by older versions.
var migrations = []string{
	`ALTER TABLE clients ADD COLUMN stage TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN attempt_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE clients ADD COLUMN last_ipxe INTEGER NOT NULL DEFAULT 0`,
}

// Open opens (and migrates) the database in dataDir.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dataDir, "ipxemultiboot.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Users

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateFirstUser creates the admin account; it fails once any user exists.
func (s *Store) CreateFirstUser(ctx context.Context, username, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrAlreadySetUp
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, string(hash), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username}, tx.Commit()
}

func (s *Store) Authenticate(ctx context.Context, username, password string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend comparable time to avoid revealing which usernames exist.
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return nil, ErrBadCredential
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrBadCredential
	}
	return &u, nil
}

func (s *Store) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrBadCredential
	}
	nh, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(nh), userID)
	return err
}

// Sessions. Only a SHA-256 of the token is stored.

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateSession(ctx context.Context, userID int64) (token string, expires time.Time, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(b)
	expires = time.Now().Add(sessionTTL)
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expires.Unix())
	return token, expires, err
}

func (s *Store) SessionUser(ctx context.Context, token string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), time.Now().Unix()).Scan(&u.ID, &u.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Store) PruneSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	return err
}

// Settings (string key/value).

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SaveSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Clients seen on the network.

type Client struct {
	MAC       string     `json:"mac"`
	IP        string     `json:"ip"`
	Firmware  string     `json:"firmware"`
	LastISO   string     `json:"lastIso"`
	LastEvent string     `json:"lastEvent"`
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	Stage     string     `json:"stage"`     // furthest stage of the latest boot attempt
	AttemptAt *time.Time `json:"attemptAt"` // when the latest attempt started
	LastIPXE  *time.Time `json:"lastIpxe"`  // last request made by iPXE (menu, boot, iPXE DHCP)
}

var stageRank = map[string]int{"dhcp": 1, "tftp": 2, "ipxe": 3, "menu": 4, "boot": 5}

// ClientUpdate is one observation of a client.
type ClientUpdate struct {
	MAC, IP, Firmware, LastISO, LastEvent string
	Stage                                 string
	IPXE                                  bool // the request came from iPXE
}

// TouchClient records an observation. A firmware DHCP request starts a new
// boot attempt; later stages only ever move the attempt forward.
func (s *Store) TouchClient(ctx context.Context, u ClientUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	var c struct {
		ip, firmware, lastISO, lastEvent, stage string
		attemptAt, lastIPXE                     int64
	}
	err = tx.QueryRowContext(ctx, `SELECT ip, firmware, last_iso, last_event, stage, attempt_at, last_ipxe FROM clients WHERE mac = ?`, u.MAC).
		Scan(&c.ip, &c.firmware, &c.lastISO, &c.lastEvent, &c.stage, &c.attemptAt, &c.lastIPXE)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return err
	}
	keep := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	keep(&c.ip, u.IP)
	keep(&c.firmware, u.Firmware)
	keep(&c.lastISO, u.LastISO)
	keep(&c.lastEvent, u.LastEvent)
	switch {
	case u.Stage == "dhcp":
		c.stage, c.attemptAt = "dhcp", now
	case stageRank[u.Stage] > stageRank[c.stage]:
		c.stage = u.Stage
		if c.attemptAt == 0 {
			c.attemptAt = now
		}
	}
	if u.IPXE {
		c.lastIPXE = now
	}
	if isNew {
		_, err = tx.ExecContext(ctx, `INSERT INTO clients (mac, ip, firmware, last_iso, last_event, first_seen, last_seen, stage, attempt_at, last_ipxe)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, u.MAC, c.ip, c.firmware, c.lastISO, c.lastEvent, now, now, c.stage, c.attemptAt, c.lastIPXE)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE clients SET ip = ?, firmware = ?, last_iso = ?, last_event = ?, last_seen = ?, stage = ?, attempt_at = ?, last_ipxe = ?
			WHERE mac = ?`, c.ip, c.firmware, c.lastISO, c.lastEvent, now, c.stage, c.attemptAt, c.lastIPXE, u.MAC)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ClientMACByIP finds the client last seen with ip (TFTP only knows the IP).
func (s *Store) ClientMACByIP(ctx context.Context, ip string) string {
	var mac string
	s.db.QueryRowContext(ctx, `SELECT mac FROM clients WHERE ip = ? ORDER BY last_seen DESC LIMIT 1`, ip).Scan(&mac)
	return mac
}

func unixPtr(v int64) *time.Time {
	if v == 0 {
		return nil
	}
	t := time.Unix(v, 0)
	return &t
}

func (s *Store) Clients(ctx context.Context) ([]Client, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mac, ip, firmware, last_iso, last_event, first_seen, last_seen, stage, attempt_at, last_ipxe
		FROM clients ORDER BY last_seen DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		var c Client
		var first, last, attempt, ipxe int64
		if err := rows.Scan(&c.MAC, &c.IP, &c.Firmware, &c.LastISO, &c.LastEvent, &first, &last, &c.Stage, &attempt, &ipxe); err != nil {
			return nil, err
		}
		c.FirstSeen, c.LastSeen = time.Unix(first, 0), time.Unix(last, 0)
		c.AttemptAt, c.LastIPXE = unixPtr(attempt), unixPtr(ipxe)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteClient(ctx context.Context, mac string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM clients WHERE mac = ?`, mac)
	return err
}
