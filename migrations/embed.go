package migrations

import "embed"

// FS contains every schema migration shipped with SynFactory.
//
//go:embed *.sql
var FS embed.FS
