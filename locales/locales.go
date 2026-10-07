// Package locales embeds the UI translations, one TOML file per language.
//
// Message IDs are nested keys ("nav.homework"). A key must not be one of
// go-i18n's reserved names (id, description, hash, zero, one, two, few, many,
// other, leftdelim, rightdelim), or its table is read as a single message.
package locales

import "embed"

//go:embed *.toml
var FS embed.FS
