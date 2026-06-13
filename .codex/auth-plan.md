# Wallchart '26 — remaining work for Codex

Core email-OTP auth is **already implemented** (login → `/auth/request` → `/auth/verify` →
session cookie; `ADMIN_EMAILS`; `COOKIE_SECURE`; team `<datalist>`; normalized team scoring;
rejoin-by-name removed). `go test ./...` and `go vet` are green.

This file lists what is **still missing or worth hardening**, ordered by priority. Each item is
independent; do them top-down. After each, run `make test` and `make build`.

---

## P1 — abuse & robustness (do first)

### 1. Rate-limit `/auth/request` beyond per-email (open email-send abuse) — DONE
Today `canIssueLoginCode` only throttles **per email** (1/min). Anyone can POST arbitrary
addresses and the server sends an OTP to each → spam relay + burns the Resend 100/day quota.
- Add a **per-IP** limit (e.g. max ~5 code requests / 15 min per client IP) and a **global daily
  cap** (e.g. ~200/day) as a safety fuse.
- Client IP: read `RemoteAddr`; if running behind Cloudflare, honor `CF-Connecting-IP`
  (preferred) or the left-most `X-Forwarded-For` **only when a trusted-proxy env flag is set**,
  else fall back to `RemoteAddr`. Do not blindly trust `X-Forwarded-For`.
- Simple in-memory sliding window (map + mutex) is fine for single-instance; no new dep.

Implemented in `main.go`: in-memory 5/15m per-IP limit, 200/24h global cap, and
`TRUST_PROXY_HEADERS=true` opt-in for `CF-Connecting-IP` / left-most `X-Forwarded-For`.

### 2. HTTP server timeouts + graceful shutdown — DONE
`run()` calls `http.ListenAndServe` directly — no timeouts (slowloris risk) and systemd's
SIGTERM kills mid-write.
- Build an `http.Server{ReadHeaderTimeout: 5s, ReadTimeout: 15s, WriteTimeout: 15s, IdleTimeout: 60s}`.
- Handle SIGINT/SIGTERM (`signal.NotifyContext`), call `srv.Shutdown(ctx)` then `db.Close()`.

Implemented in `main.go`: explicit `http.Server` timeouts and SIGINT/SIGTERM graceful shutdown.

### 3. SMTP send must not block the request / can hang — DONE
`sendLoginCode` is called synchronously inside `authRequest`; a slow SMTP server stalls the HTTP
response.
- Send with a timeout (dial via `net.Dialer{Timeout}` + `smtp` client, or run in a goroutine with
  a `context` deadline). The user-facing response should not wait on Resend.
- Note: current code relies on `smtp.SendMail` STARTTLS on port 587 (works for Resend). If anyone
  sets `SMTP_PORT=465` (implicit TLS) it will fail — either document "587 only" or add a
  `tls.Dial` path for 465.

Implemented in `main.go`: auth handler returns before SMTP completes; send runs with a 10s
context/deadline and supports STARTTLS plus implicit TLS on 465.

### 4. Dev `.env` auto-load — DONE
The Go binary reads OS env only; locally `.env` must be exported by hand. Add a tiny loader:
- On startup, if `APP_ENV` != `production` (or always, before reading config), look for `.env` in
  the working dir; if present, parse `KEY=VALUE` lines (skip blanks/`#`, strip surrounding quotes)
  and `os.Setenv` only keys **not already set** in the real environment.
- Keep it dependency-free (~20 lines). Must not override real env vars (prod/systemd wins).
- `.env`, `*.env`, `wallchart26.env` are already gitignored.

Implemented in `main.go`: local `.env` is loaded before config unless `APP_ENV=production`; real
environment variables win.

---

## P2 — operational gaps for the real tournament

### 5. Admin cannot edit kickoff time
`adminResult` updates teams + scores only. Fixture kickoff times are placeholder MVP slots, and
locking (`kickoff.After(now)`) depends on them, so real matches will lock at the wrong time.
- Add a `kickoff` field to the admin form ([templates/admin.html]) and parse/update
  `matches.kickoff_utc` in `adminResult` (validate RFC3339 / `datetime-local`).
- Optional: a one-shot "reseed fixtures from fixtures.json" admin action for when the bracket fills
  in, without wiping predictions.

### 6. Prune expired sessions
`sessions` only shrinks on logout; expired rows accumulate. (`login_codes` is fine — keyed by
email, overwritten.)
- Delete `WHERE expires_at < now` on a periodic ticker (e.g. hourly) or opportunistically on login.

---

## P3 — hardening

### 7. Security headers
No headers set today. Add via middleware wrapping the mux:
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: same-origin`
- `X-Frame-Options: DENY` (clickjacking — `/me` predictions)
- A CSP. Templates currently use **inline `<script>` and `<style>`**, so a strict CSP needs either
  `'unsafe-inline'` (weak) or moving JS/CSS to embedded static files served with a nonce/hash.
  Prefer extracting inline JS/CSS to `//go:embed`ed assets + `script-src 'self'`.

### 8. Unify enumeration-revealing message
`authRequest` returns "A code was requested recently…" on the rate-limit branch, which leaks that
an active code exists for that email. Return the same generic "Check your email…" message in both
branches.

---

## P4 — cleanup & tests

### 9. Remove dead code
- `pageData.Users` is unused in all templates — drop the field.
- `login_codes.name` is stored at request time but `authVerify` uses the form's `name` instead.
  Either use the stored name (so it survives the verify step) or stop storing it.

### 10. Tests for the auth layer
Only `score()` is covered. Add table tests for: `normalizeEmail` (valid/invalid/too-long),
`hashLoginCode`/`constantTimeEqual`, `randomDigits` (length + digits-only), `verifyLoginCode`
(success, expired, wrong code, attempts ≥ 5), and `canIssueLoginCode` (throttle window).

### 11. Optional niceties
- `GET /healthz` returning 200 for systemd/monitoring.
- Render kickoff in the viewer's local timezone (small client-side `toLocaleString` pass) instead
  of UTC-only.
- Server-side: ignore knockout team guesses that aren't in the known team list (defense in depth;
  datalist doesn't enforce).

---

## Out of scope (product decisions, not bugs)
- Email normalization ignores Gmail dots/`+tags` — two variants are distinct accounts. Acceptable.
- CSRF relies on `SameSite=Lax` (blocks cross-site POST). Add tokens only if going fully public.
