package main

import "strings"

// teamISO maps a canonical (English) team key to its ISO 3166-1 alpha-2 code
// (with UK home nations as flag-icons subdivision codes). Codes correspond to
// the square SVGs embedded under static/flags/<code>.svg.
var teamISO = map[string]string{
	"Algeria":                "dz",
	"Argentina":              "ar",
	"Australia":              "au",
	"Austria":                "at",
	"Belgium":                "be",
	"Bosnia and Herzegovina": "ba",
	"Brazil":                 "br",
	"Canada":                 "ca",
	"Cape Verde":             "cv",
	"Colombia":               "co",
	"Croatia":                "hr",
	"Curacao":                "cw",
	"Czechia":                "cz",
	"DR Congo":               "cd",
	"Ecuador":                "ec",
	"Egypt":                  "eg",
	"England":                "gb-eng",
	"France":                 "fr",
	"Germany":                "de",
	"Ghana":                  "gh",
	"Haiti":                  "ht",
	"Iran":                   "ir",
	"Iraq":                   "iq",
	"Ivory Coast":            "ci",
	"Japan":                  "jp",
	"Jordan":                 "jo",
	"Mexico":                 "mx",
	"Morocco":                "ma",
	"Netherlands":            "nl",
	"New Zealand":            "nz",
	"Norway":                 "no",
	"Panama":                 "pa",
	"Paraguay":               "py",
	"Portugal":               "pt",
	"Qatar":                  "qa",
	"Saudi Arabia":           "sa",
	"Scotland":               "gb-sct",
	"Senegal":                "sn",
	"South Africa":           "za",
	"South Korea":            "kr",
	"Spain":                  "es",
	"Sweden":                 "se",
	"Switzerland":            "ch",
	"Tunisia":                "tn",
	"Turkey":                 "tr",
	"United States":          "us",
	"Uruguay":                "uy",
	"Uzbekistan":             "uz",
}

// teamFlagISO returns the ISO code for a team key, or "" for placeholders
// (TBD…), empty values, or unknown teams — callers render no flag in that case.
func teamFlagISO(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "TBD") {
		return ""
	}
	return teamISO[key]
}
