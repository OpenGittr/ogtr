package migrations

import "gofr.dev/pkg/gofr/migration"

// addClicksOrgTsIndex adds the clicks (org_id, ts) index that backs org-wide
// usage metering (backend/usage: current-month event counts). The existing
// (org_id, link_id, ts) analytics index cannot serve an org-wide ts range —
// link_id sits between the two columns — so without this index the monthly
// COUNT would scan every click row of the org.
//
// First incremental migration: production databases exist now, so schema
// changes land as new timestamped files (the initial schema also carries this
// index for fresh installs, but initial-migration edits never re-run on
// existing databases).
func addClicksOrgTsIndex() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			// Guarded like the initial schema's CREATEs: a database that
			// already has the index (a fresh install created after the
			// initial-schema edit) skips the ALTER.
			var n int

			err := d.SQL.QueryRow(
				`SELECT COUNT(*) FROM information_schema.statistics
				 WHERE table_schema = DATABASE() AND table_name = 'clicks'
				   AND index_name = 'idx_clicks_org_ts'`).Scan(&n)
			if err != nil {
				return err
			}

			if n > 0 {
				return nil
			}

			_, err = d.SQL.Exec("ALTER TABLE clicks ADD INDEX idx_clicks_org_ts (org_id, ts)")

			return err
		},
	}
}
