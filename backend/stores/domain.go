package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// DomainStore reads and writes the domains table (per-org custom short
// domains, FEATURES.md §1.6). Every query is org-scoped except GetByHostname:
// the hostname column is a single deployment-wide namespace (one org owns a
// hostname), and the redirect path resolves the owning org FROM the hostname.
type DomainStore struct{}

// NewDomainStore builds a DomainStore.
func NewDomainStore() *DomainStore { return &DomainStore{} }

const domainColumns = "id, org_id, hostname, verification_token, status, verified_at, is_primary, created_at"

const insertDomain = "INSERT INTO domains (org_id, hostname, verification_token) VALUES (?, ?, ?)"

// Create inserts a PENDING domain and returns the stored row.
func (s *DomainStore) Create(ctx *gofr.Context, orgID int64, hostname, verificationToken string) (*models.Domain, error) {
	res, err := ctx.SQL.ExecContext(ctx, insertDomain, orgID, hostname, verificationToken)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, orgID, id)
}

// GetByID fetches a domain within the org; (nil, nil) when absent — a domain
// from another org never resolves.
func (*DomainStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.Domain, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+domainColumns+" FROM domains WHERE id = ? AND org_id = ?", id, orgID)

	return scanDomain(row)
}

// GetByHostname fetches a domain by its (globally unique) hostname;
// (nil, nil) when absent. Deliberately not org-scoped: it backs the global
// uniqueness check at creation and the host→org lookup on the redirect path.
func (*DomainStore) GetByHostname(ctx *gofr.Context, hostname string) (*models.Domain, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+domainColumns+" FROM domains WHERE hostname = ?", hostname)

	return scanDomain(row)
}

// ListByOrg returns all of the org's domains, oldest first (stable order for
// the settings UI).
func (*DomainStore) ListByOrg(ctx *gofr.Context, orgID int64) ([]models.Domain, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+domainColumns+" FROM domains WHERE org_id = ? ORDER BY id", orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	domains := []models.Domain{}

	for rows.Next() {
		d, err := scanDomainRow(rows)
		if err != nil {
			return nil, err
		}

		domains = append(domains, *d)
	}

	return domains, rows.Err()
}

// PrimaryVerifiedHostname returns the org's primary VERIFIED hostname, or ""
// when the org has none — the short-URL display base then stays the
// deployment's SHORT_DOMAIN.
func (*DomainStore) PrimaryVerifiedHostname(ctx *gofr.Context, orgID int64) (string, error) {
	var hostname string

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT hostname FROM domains WHERE org_id = ? AND is_primary = 1 AND status = ? LIMIT 1",
		orgID, models.DomainStatusVerified).Scan(&hostname)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return hostname, nil
}

// HasVerified reports whether the org owns at least one VERIFIED custom
// domain — the trigger for the relaxed (functional-only) reserved-alias
// scope: an org serving links on its own domain owns that namespace.
func (*DomainStore) HasVerified(ctx *gofr.Context, orgID int64) (bool, error) {
	var one int

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT 1 FROM domains WHERE org_id = ? AND status = ? LIMIT 1",
		orgID, models.DomainStatusVerified).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// SetVerified flips a domain to VERIFIED and stamps verified_at, org-scoped.
func (*DomainStore) SetVerified(ctx *gofr.Context, orgID, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE domains SET status = ?, verified_at = CURRENT_TIMESTAMP WHERE id = ? AND org_id = ?",
		models.DomainStatusVerified, id, orgID)

	return err
}

// SetPrimary makes this domain the org's single primary in one transaction:
// clear the org's current primary, then set the new one. The second UPDATE
// requires VERIFIED status inside the same statement, so a concurrent status
// change cannot slip an unverified primary through; when it matches no row
// (false, nil) is returned and the whole swap rolls back — the previous
// primary survives.
func (*DomainStore) SetPrimary(ctx *gofr.Context, orgID, id int64) (bool, error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx,
		"UPDATE domains SET is_primary = 0 WHERE org_id = ?", orgID); err != nil {
		return false, err
	}

	res, err := tx.ExecContext(ctx,
		"UPDATE domains SET is_primary = 1 WHERE id = ? AND org_id = ? AND status = ?",
		id, orgID, models.DomainStatusVerified)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if affected != 1 {
		return false, nil // rolled back by the deferred Rollback
	}

	return true, tx.Commit()
}

// Delete removes a domain, org-scoped. Reports whether a row was deleted.
func (*DomainStore) Delete(ctx *gofr.Context, orgID, id int64) (bool, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"DELETE FROM domains WHERE id = ? AND org_id = ?", id, orgID)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

func scanDomainRow(row rowScanner) (*models.Domain, error) {
	var d models.Domain

	err := row.Scan(&d.ID, &d.OrgID, &d.Hostname, &d.VerificationToken, &d.Status,
		&d.VerifiedAt, &d.IsPrimary, &d.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &d, nil
}

func scanDomain(row *sql.Row) (*models.Domain, error) {
	d, err := scanDomainRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return d, nil
}
