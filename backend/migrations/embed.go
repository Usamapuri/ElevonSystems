// Package migrations embeds the ordered SQL migration files. go:embed cannot
// reach outside its package directory, which is why this tiny package exists.
package migrations

import "embed"

// FS holds every NNN_name.sql in this directory.
//
//go:embed *.sql
var FS embed.FS
