// Package migrations embeds the SQL migration files so binaries can migrate the
// database without the source tree present.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
