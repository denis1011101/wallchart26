package main

import (
	"io"
	"net/http"
	"strings"
)

// siteURL is the canonical public origin, no trailing slash.
const siteURL = "https://wallchart26.com"

// canonicalURL turns a request path into an absolute URL for canonical/OG tags.
func canonicalURL(path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return siteURL + path
}

// ogLocale maps a UI language to an Open Graph locale code.
func ogLocale(lang string) string {
	if normalizeLang(lang) == "ru" {
		return "ru_RU"
	}
	return "en_US"
}

func (a *app) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// Block action/fragment/auth-gated routes that should never be crawled.
	// Public GET pages that carry a noindex meta (/login, /u/{id}) are left
	// crawlable on purpose so the noindex can be read and honored.
	io.WriteString(w, "User-agent: *\n"+
		"Disallow: /me\n"+
		"Disallow: /admin\n"+
		"Disallow: /logout\n"+
		"Disallow: /auth/\n"+
		"Disallow: /lang\n"+
		"Disallow: /leaderboard\n"+
		"\n"+
		"Sitemap: "+siteURL+"/sitemap.xml\n")
}

func (a *app) sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n"+
		"  <url>\n"+
		"    <loc>"+siteURL+"/</loc>\n"+
		"    <changefreq>daily</changefreq>\n"+
		"    <priority>1.0</priority>\n"+
		"  </url>\n"+
		"</urlset>\n")
}

func (a *app) faviconICO(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/static/favicon.svg", http.StatusMovedPermanently)
}
