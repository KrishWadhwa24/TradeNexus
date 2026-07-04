// Package migrations embeds the SQL migration files so they can be applied
// at startup without shipping a separate CLI or the raw .sql files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
