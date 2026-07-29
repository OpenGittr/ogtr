package migrations

import "gofr.dev/pkg/gofr/migration"

// addLinkGating adds the columns behind three link controls, all NULL by
// default so existing links are unchanged:
//   - activates_at / expires_at: scheduled activation and expiry. A link
//     resolves only inside its [activates_at, expires_at) window; outside it
//     the resolver serves a "not live yet" / "expired" page instead of the 302.
//   - password_hash: a bcrypt hash. When set, resolution shows a password gate
//     instead of redirecting until the visitor submits the correct password.
//
// Guarded per column like the initial schema's CREATEs, so a fresh install
// whose folded-in initial schema already carries a column skips that ALTER.
func addLinkGating() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			cols := []struct{ name, ddl string }{
				{"activates_at", "ALTER TABLE links ADD COLUMN activates_at TIMESTAMP NULL"},
				{"expires_at", "ALTER TABLE links ADD COLUMN expires_at TIMESTAMP NULL"},
				{"password_hash", "ALTER TABLE links ADD COLUMN password_hash VARCHAR(255) NULL"},
			}

			for _, c := range cols {
				var n int

				err := d.SQL.QueryRow(
					`SELECT COUNT(*) FROM information_schema.columns
					 WHERE table_schema = DATABASE() AND table_name = 'links'
					   AND column_name = ?`, c.name).Scan(&n)
				if err != nil {
					return err
				}

				if n > 0 {
					continue
				}

				if _, err := d.SQL.Exec(c.ddl); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
