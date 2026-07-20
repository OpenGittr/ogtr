package migrations

import "gofr.dev/pkg/gofr/migration"

// createInitialSchema creates the full initial schema from ARCHITECTURE.md §2:
// orgs, users, org_members, invites, links, link_rules, clicks, api_keys,
// domains, link_edits.
//
// Notes tied to the spec:
//   - links.code is a single unique namespace shared by generated codes and
//     custom aliases (FEATURES.md INV-5).
//   - clicks is indexed on (org_id, link_id, ts) and written for every
//     resolution (INV-4).
//   - links has an (org_id, destination prefix) index for per-org dedupe (§1.1).
//   - links.user_id is NULLABLE: links created via a developer API key have no
//     user; they carry api_key_id instead (phase 6, ARCHITECTURE.md §4).
//   - api_keys.key_hint is the recognizable key prefix shown in the list UI;
//     the full key exists only as a SHA-256 hex in key_hash.
//
// Dev convention: this file is edited in place between production releases
// (phase 6 added api_keys.key_hint and made links.user_id nullable; the geo
// click-stats work added clicks.country/region; the destination-editing work
// added the link_edits audit table; the custom-domains work added the domains
// table; the abuse-protection work added links.status and the abuse_reports
// table); local dev databases get the equivalent ALTERs/CREATEs applied
// directly.
func createInitialSchema() migration.Migrate {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS orgs (
			id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			name             VARCHAR(255)    NOT NULL,
			slug             VARCHAR(100)    NOT NULL,
			auto_join_domain VARCHAR(255)    NULL,
			created_at       TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_orgs_slug (slug)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS users (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			name       VARCHAR(255)    NOT NULL,
			email      VARCHAR(255)    NOT NULL,
			status     VARCHAR(20)     NOT NULL DEFAULT 'ENABLED',
			created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_users_email (email)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS org_members (
			org_id     BIGINT UNSIGNED         NOT NULL,
			user_id    BIGINT UNSIGNED         NOT NULL,
			role       ENUM('OWNER','MEMBER')  NOT NULL DEFAULT 'MEMBER',
			created_at TIMESTAMP               NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (org_id, user_id),
			KEY idx_org_members_user (user_id),
			CONSTRAINT fk_org_members_org  FOREIGN KEY (org_id)  REFERENCES orgs(id),
			CONSTRAINT fk_org_members_user FOREIGN KEY (user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS invites (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			org_id     BIGINT UNSIGNED NOT NULL,
			email      VARCHAR(255)    NOT NULL,
			invited_by BIGINT UNSIGNED NOT NULL,
			status     VARCHAR(20)     NOT NULL DEFAULT 'PENDING',
			created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_invites_org (org_id),
			KEY idx_invites_email (email),
			CONSTRAINT fk_invites_org        FOREIGN KEY (org_id)     REFERENCES orgs(id),
			CONSTRAINT fk_invites_invited_by FOREIGN KEY (invited_by) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS api_keys (
			id           BIGINT UNSIGNED             NOT NULL AUTO_INCREMENT,
			org_id       BIGINT UNSIGNED             NOT NULL,
			name         VARCHAR(255)                NOT NULL,
			key_hash     VARCHAR(255)                NOT NULL,
			key_hint     VARCHAR(20)                 NOT NULL,
			status       ENUM('ENABLED','DISABLED')  NOT NULL DEFAULT 'ENABLED',
			created_at   TIMESTAMP                   NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP                   NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uq_api_keys_key_hash (key_hash),
			KEY idx_api_keys_org (org_id),
			CONSTRAINT fk_api_keys_org FOREIGN KEY (org_id) REFERENCES orgs(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS links (
			id              BIGINT UNSIGNED           NOT NULL AUTO_INCREMENT,
			org_id          BIGINT UNSIGNED           NOT NULL,
			user_id         BIGINT UNSIGNED           NULL,
			api_key_id      BIGINT UNSIGNED           NULL,
			code            VARCHAR(50)               NOT NULL,
			destination_url TEXT                      NOT NULL,
			type            ENUM('PUBLIC','PRIVATE')  NOT NULL DEFAULT 'PUBLIC',
			status          ENUM('ACTIVE','DISABLED_ABUSE') NOT NULL DEFAULT 'ACTIVE',
			utm_source      VARCHAR(255)              NULL,
			utm_medium      VARCHAR(255)              NULL,
			utm_campaign    VARCHAR(255)              NULL,
			deeplink_config JSON                      NULL,
			visits          BIGINT UNSIGNED           NOT NULL DEFAULT 0,
			last_visit_at   TIMESTAMP                 NULL,
			created_at      TIMESTAMP                 NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_links_code (code),
			KEY idx_links_org_user (org_id, user_id),
			KEY idx_links_org_destination (org_id, destination_url(255)),
			CONSTRAINT fk_links_org     FOREIGN KEY (org_id)     REFERENCES orgs(id),
			CONSTRAINT fk_links_user    FOREIGN KEY (user_id)    REFERENCES users(id),
			CONSTRAINT fk_links_api_key FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS link_rules (
			id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			org_id      BIGINT UNSIGNED NOT NULL,
			link_id     BIGINT UNSIGNED NOT NULL,
			target_name VARCHAR(255)    NOT NULL,
			rule        JSON            NOT NULL,
			created_at  TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_link_rules_org_link (org_id, link_id),
			CONSTRAINT fk_link_rules_org  FOREIGN KEY (org_id)  REFERENCES orgs(id),
			CONSTRAINT fk_link_rules_link FOREIGN KEY (link_id) REFERENCES links(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS clicks (
			id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			org_id         BIGINT UNSIGNED NOT NULL,
			link_id        BIGINT UNSIGNED NOT NULL,
			ts             TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			utm_source     VARCHAR(255)    NULL,
			utm_medium     VARCHAR(255)    NULL,
			utm_campaign   VARCHAR(255)    NULL,
			device_type    VARCHAR(50)     NULL,
			mobile_os      VARCHAR(50)     NULL,
			browser        VARCHAR(50)     NULL,
			browser_grade  VARCHAR(10)     NULL,
			referrer       VARCHAR(2048)   NULL,
			ip             VARCHAR(45)     NULL,
			city           VARCHAR(255)    NULL,
			country        VARCHAR(100)    NULL,
			region         VARCHAR(100)    NULL,
			is_deeplink    BOOLEAN         NOT NULL DEFAULT FALSE,
			target_matched BOOLEAN         NOT NULL DEFAULT FALSE,
			custom_tag_id  VARCHAR(255)    NULL,
			PRIMARY KEY (id),
			KEY idx_clicks_org_link_ts (org_id, link_id, ts),
			CONSTRAINT fk_clicks_link FOREIGN KEY (link_id) REFERENCES links(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS domains (
			id                 BIGINT UNSIGNED                        NOT NULL AUTO_INCREMENT,
			org_id             BIGINT UNSIGNED                        NOT NULL,
			hostname           VARCHAR(255)                           NOT NULL,
			verification_token VARCHAR(64)                            NOT NULL,
			status             ENUM('PENDING','VERIFIED','DISABLED')  NOT NULL DEFAULT 'PENDING',
			verified_at        TIMESTAMP                              NULL,
			is_primary         TINYINT(1)                             NOT NULL DEFAULT 0,
			created_at         TIMESTAMP                              NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_domains_hostname (hostname),
			KEY idx_domains_org (org_id),
			CONSTRAINT fk_domains_org FOREIGN KEY (org_id) REFERENCES orgs(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS abuse_reports (
			id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			org_id           BIGINT UNSIGNED NOT NULL,
			link_id          BIGINT UNSIGNED NOT NULL,
			code             VARCHAR(50)     NOT NULL,
			reason           VARCHAR(140)    NOT NULL,
			reporter_contact VARCHAR(255)    NULL,
			created_at       TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_abuse_reports_org_link (org_id, link_id),
			CONSTRAINT fk_abuse_reports_org  FOREIGN KEY (org_id)  REFERENCES orgs(id),
			CONSTRAINT fk_abuse_reports_link FOREIGN KEY (link_id) REFERENCES links(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS link_edits (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			org_id     BIGINT UNSIGNED NOT NULL,
			link_id    BIGINT UNSIGNED NOT NULL,
			user_id    BIGINT UNSIGNED NOT NULL,
			old_url    TEXT            NOT NULL,
			new_url    TEXT            NOT NULL,
			created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_link_edits_org_link (org_id, link_id),
			CONSTRAINT fk_link_edits_org  FOREIGN KEY (org_id)  REFERENCES orgs(id),
			CONSTRAINT fk_link_edits_link FOREIGN KEY (link_id) REFERENCES links(id),
			CONSTRAINT fk_link_edits_user FOREIGN KEY (user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}

	return migration.Migrate{
		UP: func(d migration.Datasource) error {
			for _, stmt := range stmts {
				if _, err := d.SQL.Exec(stmt); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
