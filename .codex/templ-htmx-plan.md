# Wallchart '26 — migrate to Go + templ + htmx (plan for Codex)

Move rendering from `html/template` to **templ** (type-safe, compiled), and add **htmx** for the two
interactions where it earns its keep (autosave, live leaderboard). Do it in phases; **each phase
ends green** (`make test`, `make build`, `go vet ./...`) and leaves the app fully working. Don't
start a phase until the previous one is at parity.

This is a behavior-preserving migration except for two deliberate UX upgrades in Phase 2 (htmx
autosave + leaderboard auto-refresh). Everything else must look and behave exactly as today.

---

## Decisions (read first)

1. **`.templ` files live in the repo root, `package main`.** Keeping them in `package main` means
   components call the existing unexported helpers and types directly (`teamName`, `t`, `plural`,
   `stageLabel`, `pageData`, `matchRow`) with **zero new package boundaries / exports**. The cost is
   more files in root (~7 `.templ` + generated `*_templ.go`). We accept that — a separate `view`
   package would force exporting the shared types and risk import cycles, which is exactly the
   boundary cost we avoided when splitting `main.go`. (If root clutter really bites later, that's a
   separate refactor.)
2. **Commit the generated `*_templ.go` files.** So `go build` and the systemd deploy work without
   `templ` installed on the target. Add `//go:generate templ generate` and run it in the Makefile.
3. **Vendor htmx locally**, don't CDN it. Embed `static/htmx.min.js` and serve it from `/static/`.
   Matches the app's "no external deps, no third-party requests" ethos and keeps a future CSP simple.
4. **CSS**: extract the current `{{define "styles"}}` block (~170 lines) into an embedded
   `static/app.css` served at `/static/app.css`, linked with `<link rel="stylesheet">`. Cleaner than
   inlining a huge string in a templ component, and CSP-friendly. (The small page-specific inline
   `<script>`s — intro modal, timefmt — stay inline for now.)

---

## Phase 0 — tooling & static serving

- `go get github.com/a-h/templ` and install the CLI (`go install github.com/a-h/templ/cmd/templ@latest`);
  pin a version. Go 1.22 is fine.
- Create `static/` and an embed: `//go:embed static` → serve via
  `mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))`.
  Set long `Cache-Control` on these (immutable assets) — optional.
- Add `htmx.min.js` (pinned version) into `static/`.
- Move the CSS out of `partials.html`'s `styles` block into `static/app.css` (verbatim).
- Makefile: add a `generate` step (`templ generate`) and make `build`/`test`/`run` depend on it.
  Update README build instructions (note: needs `templ` CLI for dev; generated files are committed).
- End state: app still renders via html/template (unchanged), but `/static/app.css` and
  `/static/htmx.min.js` are now served. Green.

## Phase 1 — templ migration to parity (the big mechanical step)

Convert each template 1:1. No visual or behavior change.

- **Layout**: one `layout.templ` component taking `pageData` (or a small `layoutProps`) and a
  `templ.Component` body via `{ children... }`. It renders `<!doctype html>`, `<head>` with
  `<link rel="stylesheet" href="/static/app.css">`, `<html lang={ data.Lang }>`, the htmx
  `<script src="/static/htmx.min.js">` (add now, used in Phase 2), then the header and `{ children... }`.
- **Header / timefmt**: `partials.html`'s `{{define "header"}}` → `header.templ` component;
  `{{define "timefmt"}}` script → a `timefmt.templ` component (keep the JS as-is inside it).
- **Pages**: `home.templ`, `me.templ`, `user.templ`, `admin.templ`, `login.templ`, `verify.templ`,
  each a `templ.Component` func taking the typed `pageData`.
- **Funcmap → Go calls.** Every `{{func ...}}` becomes a normal call inside `{ ... }`:
  `{{teamName .Home $.Lang}}` → `{ teamName(m.Home, data.Lang) }`; `{{t .Lang "x"}}` →
  `{ t(data.Lang, "x") }`; `{{add $i 1}}` → use templ's range index `i + 1`; `{{stageLabel . $.Lang}}`
  → `{ stageLabel(m, data.Lang) }`. The whole `template.FuncMap` (main.go:132) and the
  `scoreText/formatKickoff/...` funcs stay as plain Go functions — just no longer registered.
- **Escaping note**: `html/template` auto-escapes; templ also auto-escapes `{ expr }`. Watch the
  one raw spot — `match.vs` ru value is `—` and the score dash; these are plain text, fine. Any
  intentional raw HTML would need `templ.Raw` (there is none today).
- **Swap rendering**: replace `a.render(w, r, name, data)` (handlers.go:428) and
  `a.templates.ExecuteTemplate` (436) with `component.Render(r.Context(), w)`. Keep a thin
  `a.render` wrapper that sets `Content-Type: text/html; charset=utf-8`, fills `data.Lang`/`Timezone`
  defaults (as the current render does, handlers.go ~430), writes status, and calls
  `page(data).Render(...)`. Each handler picks its page component.
- **Delete** the `templates *template.Template` field (main.go:46), the `ParseFS` setup
  (main.go:132-148), the funcmap, and the `//go:embed templates/*.html` (keep `fixtures.json` embed).
  Delete the `templates/*.html` files once parity is confirmed.
- **Verify parity**: boot the server, diff the rendered HTML of `/`, `/login`, `/me`, `/u/{id}`,
  `/admin` against the pre-migration output (curl + save before/after; ignore only whitespace
  differences templ introduces). Green test/vet/build.

## Phase 2 — htmx where it pays off

- **Autosave on `/me`** (replaces the hand-rolled `fetch` in me.html): on each predict form use
  `hx-post="/predict"`, `hx-trigger="change delay:400ms, blur"`, `hx-swap="outerHTML"` targeting that
  row's status cell, and return a **small templ fragment** (the `<div class="status">…</div>` with
  Saved/Locked/Result text) instead of plain `200`. Keep the "don't submit half-filled" guard
  server-side (it already validates). Remove the bespoke save() JS. Keep the disabled-when-locked
  behavior.
  - Gotcha: keep the autosave UX (Saving…/Saved) via `hx-indicator` or by returning the status text.
- **Live leaderboard** on `/`: add `GET /leaderboard` returning **only** the leaderboard table
  fragment (reuse the same templ component the page uses). On the table add
  `hx-get="/leaderboard" hx-trigger="every 30s" hx-swap="outerHTML"`. Real value during matches when
  the admin is entering results.
- **htmx + the timefmt script**: the `<time data-ts>` formatting runs on page load today. After an
  htmx swap, swapped-in content with `<time data-ts>` won't be re-formatted. The leaderboard
  fragment has no `<time>`, so it's fine — but if any future fragment includes times, re-run the
  formatter on `htmx:afterSwap` (or use `htmx.onLoad`). Note this in a comment.
- Keep `/lang` and `/tz` as full navigations (htmx-boosting them is not worth the edge cases).

## Phase 3 — cleanup

- Remove dead helpers only if truly unused after the migration (don't prune anything still called).
- README: document `templ generate` in the build flow and that generated files are committed.
- Confirm the systemd/static-binary deploy is unaffected: `CGO_ENABLED=0 go build` still yields one
  static binary (generated `_templ.go` + embedded `static/` are compiled in). `templ` is a
  build-time-only dependency.

---

## Verification (every phase)
- `make test`, `make build`, `go vet ./...` green; existing tests (`auth_test`, `score_test`,
  `i18n_test`) untouched and passing.
- `COOKIE_SECURE=false go run .` boots; manually hit `/`, `/login`, `/me` (logged in), `/admin`.
- Phase 1: HTML parity vs saved pre-migration output (whitespace-only diffs allowed).
- Phase 2: autosave still persists predictions (check DB / reload), leaderboard refreshes without a
  full reload, locked matches stay locked.

## Out of scope
- No change to auth, scoring, DB schema, or i18n logic — only the rendering layer + 2 htmx features.
- No move to a separate `view` package (see Decision 1).
- No SPA / client routing — htmx is hypermedia, server stays the source of truth.
