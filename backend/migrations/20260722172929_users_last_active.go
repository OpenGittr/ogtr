package migrations

import "gofr.dev/pkg/gofr/migration"

// addUsersLastActive adds users.last_active_at — the coarse activity signal
// behind the instance-admin listings. NULL means "never seen since the column
// existed"; the auth service touches it on login and token refresh, throttled
// to at most one write per hour per user (a conditional UPDATE), so the
// column is telemetry, not an audit trail. No index: nothing filters or
// sorts on it — it is only ever read alongside rows already selected by
// other keys, and the throttled touch is a primary-key UPDATE.
func addUsersLastActive() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Guarded like the initial schema's CREATEs: a fresh install
			// whose initial schema already carries the column skips the
			// ALTER.
			var n int

			err := d.SQL.QueryRow(
				`SELECT COUNT(*) FROM information_schema.columns
				 WHERE table_schema = DATABASE() AND table_name = 'users'
				   AND column_name = 'last_active_at'`).Scan(&n)
			if err != nil {
				return err
			}

			if n > 0 {
				return nil
			}

			_, err = d.SQL.Exec("ALTER TABLE users ADD COLUMN last_active_at TIMESTAMP NULL")

			return err
		},
	}
}
