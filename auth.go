package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"
)

type smtpConfig struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

type loginRequestLimiter struct {
	mu           sync.Mutex
	byIP         map[string][]time.Time
	global       []time.Time
	perIPLimit   int
	perIPWindow  time.Duration
	globalLimit  int
	globalWindow time.Duration
}

func (a *app) canIssueLoginCode(ctx context.Context, emailNorm string, now time.Time) bool {
	var createdRaw string
	err := a.db.QueryRowContext(ctx, `SELECT created_at FROM login_codes WHERE email_norm = ?`, emailNorm).Scan(&createdRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		log.Printf("login rate check: %v", err)
		return false
	}
	created, err := time.Parse(time.RFC3339, createdRaw)
	return err == nil && now.Sub(created) >= time.Minute
}

func newLoginRequestLimiter(perIPLimit int, perIPWindow time.Duration, globalLimit int, globalWindow time.Duration) *loginRequestLimiter {
	return &loginRequestLimiter{
		byIP:         map[string][]time.Time{},
		perIPLimit:   perIPLimit,
		perIPWindow:  perIPWindow,
		globalLimit:  globalLimit,
		globalWindow: globalWindow,
	}
}

func (l *loginRequestLimiter) Allow(ip string, now time.Time) bool {
	if l == nil {
		return true
	}
	if ip == "" {
		ip = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.global = pruneWindow(l.global, now.Add(-l.globalWindow))
	if len(l.global) >= l.globalLimit {
		return false
	}

	history := pruneWindow(l.byIP[ip], now.Add(-l.perIPWindow))
	if len(history) >= l.perIPLimit {
		l.byIP[ip] = history
		return false
	}

	l.byIP[ip] = append(history, now)
	l.global = append(l.global, now)
	return true
}

func pruneWindow(items []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(items) && items[first].Before(cutoff) {
		first++
	}
	if first == 0 {
		return items
	}
	return append(items[:0], items[first:]...)
}

func (a *app) clientIP(r *http.Request) string {
	if a.trustProxyHeaders {
		if ip := parseIPHeader(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := parseIPHeader(strings.Split(xff, ",")[0]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	if ip := net.ParseIP(strings.TrimSpace(r.RemoteAddr)); ip != nil {
		return ip.String()
	}
	return r.RemoteAddr
}

func parseIPHeader(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func (a *app) verifyLoginCode(ctx context.Context, emailNorm, code string) bool {
	var codeHash, expiresRaw string
	var attempts int
	err := a.db.QueryRowContext(ctx, `
SELECT code_hash, expires_at, attempts FROM login_codes WHERE email_norm = ?
`, emailNorm).Scan(&codeHash, &expiresRaw, &attempts)
	if err != nil {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresRaw)
	if err != nil || !expiresAt.After(a.now()) || attempts >= 5 {
		return false
	}
	got := hashLoginCode(emailNorm, code)
	if !constantTimeEqual(got, codeHash) {
		_, _ = a.db.ExecContext(ctx, `UPDATE login_codes SET attempts = attempts + 1 WHERE email_norm = ?`, emailNorm)
		return false
	}
	_, _ = a.db.ExecContext(ctx, `DELETE FROM login_codes WHERE email_norm = ?`, emailNorm)
	return true
}

func (a *app) findOrCreateUser(ctx context.Context, email, emailNorm, name, lang string) (int64, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email_norm = ?`, emailNorm).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		token, err := randomToken()
		if err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `
INSERT INTO users(name, email, email_norm, token, lang, created_at) VALUES (?, ?, ?, ?, ?, ?)
`, name, email, emailNorm, token, lang, a.now().Format(time.RFC3339))
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE users SET email = ?, name = CASE WHEN ? <> '' THEN ? ELSE name END, lang = ? WHERE id = ?
`, email, name, name, lang, id)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (a *app) isAdmin(u *user) bool {
	if u == nil || u.EmailNorm == "" {
		return false
	}
	_, ok := a.adminEmails[u.EmailNorm]
	return ok
}

func (a *app) sendLoginCodeAsync(email, code, lang string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), smtpSendTimeout)
		defer cancel()
		if err := a.sendLoginCode(ctx, email, code, lang); err != nil {
			log.Printf("send login code: %v", err)
		}
	}()
}

func (a *app) sendLoginCode(ctx context.Context, email, code, lang string) error {
	if a.smtp.Host == "" {
		log.Printf("login code for %s: %s", email, code)
		return nil
	}
	return a.sendEmail(ctx, email, emailSubject(lang), emailBody(lang, code))
}

// sendEmail delivers a plain-text message to a single recipient over the
// configured SMTP server. When SMTP is unconfigured it logs and returns nil so
// local/dev runs don't fail.
func (a *app) sendEmail(ctx context.Context, to, subject, body string) error {
	if a.smtp.Host == "" {
		log.Printf("email to %s: %s", to, subject)
		return nil
	}
	from := a.smtp.From
	if from == "" {
		from = a.smtp.User
	}
	// The From: header may carry a display name ("Name <addr>"), but the SMTP
	// envelope (MAIL FROM) needs a bare address, else servers reject it (501).
	envelopeFrom := from
	if parsed, err := mail.ParseAddress(from); err == nil {
		envelopeFrom = parsed.Address
	}
	addr := a.smtp.Host + ":" + a.smtp.Port
	var auth smtp.Auth
	if a.smtp.User != "" {
		auth = smtp.PlainAuth("", a.smtp.User, a.smtp.Pass, a.smtp.Host)
	}
	// Subjects and bodies contain UTF-8 (incl. Cyrillic). Headers must be
	// ASCII, so the Subject is RFC 2047 encoded; the body is declared UTF-8.
	msg := strings.Join([]string{
		"To: " + to,
		"From: " + from,
		"Subject: " + mime.BEncoding.Encode("UTF-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=\"UTF-8\"",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
	}, "\r\n")
	return sendMail(ctx, a.smtp.Host, a.smtp.Port, addr, auth, envelopeFrom, []string{to}, []byte(msg))
}

func sendMail(ctx context.Context, host, port, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	dialer := net.Dialer{}
	var conn net.Conn
	var err error
	if port == "465" {
		conn, err = (&tls.Dialer{
			NetDialer: &dialer,
			Config:    &tls.Config{ServerName: host},
		}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()

	if port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err = c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return err
		}
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = wc.Write(msg); err != nil {
		_ = wc.Close()
		return err
	}
	if err = wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func normalizeEmail(raw string) (string, string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) > 254 {
		return "", "", errors.New("email too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", "", errors.New("invalid email")
	}
	return email, email, nil
}

func parseEmailSet(raw string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		_, emailNorm, err := normalizeEmail(part)
		if err == nil {
			set[emailNorm] = struct{}{}
		}
	}
	return set
}

func loadSMTPConfig() smtpConfig {
	port := getenv("SMTP_PORT", "587")
	return smtpConfig{
		Host: strings.TrimSpace(os.Getenv("SMTP_HOST")),
		Port: port,
		User: strings.TrimSpace(os.Getenv("SMTP_USER")),
		Pass: os.Getenv("SMTP_PASS"),
		From: strings.TrimSpace(os.Getenv("SMTP_FROM")),
	}
}

func randomDigits(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}

func hashLoginCode(emailNorm, code string) string {
	sum := sha256.Sum256([]byte(emailNorm + ":" + strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
