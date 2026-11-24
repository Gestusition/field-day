// Package migrations embeds the SQL schema migrations so the API binary can
// apply them without shipping the files separately.
package migrations

import "embed"

// FS holds every *.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
