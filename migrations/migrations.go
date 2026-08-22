// Package migrations is the single source of truth for RelayDB schema
// migrations. The SQL files in this directory are embedded at build time and
// consumed by internal/persistence's Migrator; there is no second copy to
// keep in sync.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
