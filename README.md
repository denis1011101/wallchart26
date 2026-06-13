# Wallchart '26

Office World Cup score sheet, online: no passwords, cookie tokens, manual results.

## Run locally

```sh
make run
```

Open `http://127.0.0.1:8080`.

Local development needs the `templ` CLI for regeneration:

```sh
/usr/local/go/bin/go install github.com/a-h/templ/cmd/templ@v0.2.793
```

The generated `*_templ.go` files are committed, so the deployed app does not need the CLI.
After editing `.templ` files, run:

```sh
make generate
```

## Environment

- `ADDR`: listen address, default `:8080`
- `DATABASE_PATH`: SQLite file, default `wallchart26.sqlite`
- `COOKIE_SECURE`: set to `false` for local HTTP; default is `true`
- `ADMIN_EMAILS`: comma-separated admin email allowlist
- `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`: optional email delivery settings
- `TRUST_PROXY_HEADERS`: set to `true` only behind a trusted proxy such as Cloudflare; then `CF-Connecting-IP` or left-most `X-Forwarded-For` is used for login rate limits
- `APP_ENV=production`: disables local `.env` auto-loading

For a persistent deploy, use an absolute `DATABASE_PATH`. SQLite will create `-wal` and `-shm` files next to the database, and Wallchart creates the parent directory automatically.

If `SMTP_HOST` is empty, login codes are printed to stdout. That is handy for local development, but production should set SMTP. `SMTP_PORT` defaults to `587`; implicit TLS on `465` is also supported.

In local development the binary auto-loads `.env` from the working directory without overriding real environment variables.

## Build & Run

Build a static binary:

```sh
make build
```

If the target server has a different architecture:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 /usr/local/go/bin/go build -buildvcs=false -o wallchart26 .
```

Run it:

```sh
ADMIN_EMAILS=you@example.com DATABASE_PATH=/opt/wallchart26/data/wallchart26.sqlite ADDR=:8080 ./wallchart26
```

`fixtures.json`, generated templ views, and `static/` assets are embedded, so the binary is enough to run the app.

## systemd

1. Build `wallchart26`.
2. Put it in `/opt/wallchart26/wallchart26`.
3. Create `/opt/wallchart26/wallchart26.env`:

```sh
ADMIN_EMAILS=you@example.com
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USER=wallchart@example.com
SMTP_PASS=change-me
SMTP_FROM=wallchart@example.com
```

4. Install `deploy/wallchart26.service` into `/etc/systemd/system/wallchart26.service`.
5. Start it:

```sh
systemctl daemon-reload
systemctl enable --now wallchart26
```
