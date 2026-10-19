package migrations

import "gofr.dev/pkg/gofr/migration"

// createBrandPrefixesTable is the brand_prefixes DDL (FEATURES.md §1.7), shared
// by this migration and the folded-in initial schema. prefix carries an
// explicit case-insensitive collation: uniqueness and resolution ignore case
// (ACMEIN and acmein can never belong to different orgs) while the value is
// stored and displayed as entered.
const createBrandPrefixesTable = `CREATE TABLE IF NOT EXISTS brand_prefixes (
			id              BIGINT UNSIGNED                                    NOT NULL AUTO_INCREMENT,
			org_id          BIGINT UNSIGNED                                    NOT NULL,
			prefix          VARCHAR(20) COLLATE utf8mb4_0900_ai_ci             NOT NULL,
			status          ENUM('PENDING','APPROVED','REJECTED','REVOKED')   NOT NULL DEFAULT 'PENDING',
			is_primary      TINYINT(1)                                         NOT NULL DEFAULT 0,
			registry        VARCHAR(16)                                        NOT NULL,
			entity_id       VARCHAR(32)                                        NOT NULL,
			header          VARCHAR(20)                                        NOT NULL,
			entity_name     VARCHAR(255)                                       NOT NULL,
			decision_reason VARCHAR(500)                                       NULL,
			decided_at      TIMESTAMP                                          NULL,
			created_at      TIMESTAMP                                          NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_brand_prefixes_prefix (prefix),
			KEY idx_brand_prefixes_org (org_id),
			KEY idx_brand_prefixes_status (status, id),
			CONSTRAINT fk_brand_prefixes_org FOREIGN KEY (org_id) REFERENCES orgs(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// addBrandPrefixes creates brand_prefixes and adds the clicks entry-point
// columns (entry_point ROOT|PREFIX|DOMAIN, brand_prefix_id — no FK: a
// released prefix's row is deleted while its clicks remain). Both clicks
// columns are NULL on rows recorded before this migration.
//
// Guarded per column like addLinkGating, so a fresh install whose folded-in
// initial schema already carries the columns skips the ALTERs.
func addBrandPrefixes() migration.Migrate {
	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			if _, err := d.SQL.Exec(createBrandPrefixesTable); err != nil {
				return err
			}

			cols := []struct{ name, ddl string }{
				{"entry_point", "ALTER TABLE clicks ADD COLUMN entry_point VARCHAR(8) NULL"},
				{"brand_prefix_id", "ALTER TABLE clicks ADD COLUMN brand_prefix_id BIGINT UNSIGNED NULL"},
			}

			for _, c := range cols {
				var n int

				err := d.SQL.QueryRow(
					`SELECT COUNT(*) FROM information_schema.columns
					 WHERE table_schema = DATABASE() AND table_name = 'clicks'
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
