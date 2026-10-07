// Package toskar holds files the whole module shares.
package toskar

import "embed"

// Catalog is the shared translation catalog (i18n/README.md):
// i18n/languages.json, i18n/locales/<language>/<namespace>.json, and the
// words automation requests are read with, i18n/requests/<language>.json. Core
// reads it to write text that leaves the app in the App language, such as
// email and push notifications; internal/locale looks text up in it.
//
//go:embed i18n/languages.json i18n/locales i18n/requests
var Catalog embed.FS
