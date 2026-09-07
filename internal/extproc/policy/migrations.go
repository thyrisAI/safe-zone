package policy

import "embed"

// MigrationFiles exposes the versioned SQL files to the repository migration
// runner that will be introduced with policy persistence.
//
//go:embed migrations/*.sql
var MigrationFiles embed.FS
