// Package migrations holds the Gofr database migrations for backend.
//
// Convention (see ARCHITECTURE.md §8): one migration file per production
// deployment window; during development we amend the pending file rather than
// stacking new ones. Filename/key prefixes come from `date +%Y%m%d%H%M%S`.
package migrations

import "gofr.dev/pkg/gofr/migration"

// All returns every migration keyed by its timestamp, for app.Migrate.
func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20260713221852: createInitialSchema(),
	}
}
