// Package migrations holds the Gofr database migrations for backend.
//
// Convention (see ARCHITECTURE.md §8): production databases exist, so every
// schema change is a NEW timestamped migration file — the pre-release
// edit-the-initial-file-in-place convention is retired. Fresh installs also
// get additive changes folded into the initial schema (so one file builds a
// new database), but the incremental file is what moves running deployments
// forward: initial-migration edits never re-run on existing databases.
// Filename/key prefixes come from `date +%Y%m%d%H%M%S` — never invented.
package migrations

import "gofr.dev/pkg/gofr/migration"

// All returns every migration keyed by its timestamp, for app.Migrate.
func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20260713221852: createInitialSchema(),
		20260721020158: addClicksOrgTsIndex(),
		20260722172929: addUsersLastActive(),
	}
}
