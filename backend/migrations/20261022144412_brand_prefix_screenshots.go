package migrations

import "gofr.dev/pkg/gofr/migration"

// createBrandPrefixScreenshotsTable is the brand_prefix_screenshots DDL
// (FEATURES.md §1.7), shared by this migration and the folded-in initial
// schema: one DLT-portal screenshot per prefix request, the evidence the
// operator reviews. Kept out of brand_prefixes so list queries never touch
// the image bytes; ON DELETE CASCADE drops it with its request (withdraw,
// release, or a rejected row replaced by a new request).
const createBrandPrefixScreenshotsTable = `CREATE TABLE IF NOT EXISTS brand_prefix_screenshots (
			prefix_id    BIGINT UNSIGNED NOT NULL,
			content_type VARCHAR(32)     NOT NULL,
			image        MEDIUMBLOB      NOT NULL,
			created_at   TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (prefix_id),
			CONSTRAINT fk_brand_prefix_screenshots_prefix FOREIGN KEY (prefix_id)
				REFERENCES brand_prefixes(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// addBrandPrefixScreenshots creates brand_prefix_screenshots. CREATE TABLE IF
// NOT EXISTS makes it a no-op on fresh installs, whose initial schema already
// has the table.
func addBrandPrefixScreenshots() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(createBrandPrefixScreenshotsTable)

			return err
		},
	}
}
