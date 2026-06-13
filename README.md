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

## Production deploy (wallchart26.com)

Live setup on the shared droplet (`46.101.126.4`). The app listens on
`127.0.0.1:8090` (port `8080` is taken by headscale); nginx terminates TLS and
proxies to it. Config templates live in `deploy/`. The systemd unit uses
`DynamicUser` + `StateDirectory`, so the SQLite DB lives at
`/var/lib/wallchart26/`.

### First-time setup

1. **DNS:** point `wallchart26.com` and `www` (A records) at the server. If the
   domain is on Cloudflare, set them to **DNS only** (grey cloud) so certbot's
   HTTP-01 challenge reaches the origin. Verify with
   `dig +short wallchart26.com @1.1.1.1`.
2. **Copy artifacts** (from the repo root):

   ```sh
   scp wallchart26 deploy/wallchart26.service deploy/wallchart26.nginx.conf root@46.101.126.4:/root/
   ```

3. **Install the app** (on the server):

   ```sh
   install -d /opt/wallchart26
   install -m 0755 /root/wallchart26 /opt/wallchart26/wallchart26
   # secrets only (SMTP_*, ADMIN_EMAILS); other config is in the unit
   vim /opt/wallchart26/wallchart26.env        # see deploy/wallchart26.env.example
   chmod 600 /opt/wallchart26/wallchart26.env
   install -m 0644 /root/wallchart26.service /etc/systemd/system/wallchart26.service
   systemctl daemon-reload
   systemctl enable --now wallchart26
   curl -sI http://127.0.0.1:8090/ | head -3   # expect HTTP/1.1 200 OK
   ```

4. **nginx + Let's Encrypt** (on the server):

   ```sh
   # temporary HTTP block for the ACME challenge
   cat > /etc/nginx/sites-available/wallchart26.com <<'EOF'
   server {
       listen 80;
       listen [::]:80;
       server_name wallchart26.com www.wallchart26.com;
       location /.well-known/acme-challenge/ { allow all; root /var/www/html; }
       location / { return 301 https://wallchart26.com$request_uri; }
   }
   EOF
   ln -sf /etc/nginx/sites-available/wallchart26.com /etc/nginx/sites-enabled/
   nginx -t && systemctl reload nginx

   certbot certonly --webroot -w /var/www/html -d wallchart26.com -d www.wallchart26.com

   # full HTTPS config proxying to :8090
   cp /root/wallchart26.nginx.conf /etc/nginx/sites-available/wallchart26.com
   nginx -t && systemctl reload nginx
   ```

Certbot installs a renewal timer, so the certificate renews automatically.

### Redeploy a new version

```sh
make build                                   # local
scp wallchart26 root@46.101.126.4:/root/     # local
```

```sh
install -m 0755 /root/wallchart26 /opt/wallchart26/wallchart26   # server
systemctl restart wallchart26
```

The database (`/var/lib/wallchart26/`), nginx config, and certificate are left
untouched. Check logs with `journalctl -u wallchart26 -n 50 --no-pager`.
