package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// BrandPrefixStore reads and writes the brand_prefixes table (path-scoped
// entry points on the shared short domain, FEATURES.md §1.7). Every query is
// org-scoped except GetByPrefix and IsHeld: the prefix column is a single
// deployment-wide namespace, and the redirect path resolves the owning org
// FROM the prefix. Operator decisions (cross-org) live in AdminStore.
//
// Comparisons on prefix are case-insensitive through the column's collation.
type BrandPrefixStore struct{}

// NewBrandPrefixStore builds a BrandPrefixStore.
func NewBrandPrefixStore() *BrandPrefixStore { return &BrandPrefixStore{} }

const brandPrefixColumns = "id, org_id, prefix, status, is_primary, registry, entity_id, header, " +
	"entity_name, decision_reason, decided_at, created_at, " +
	"EXISTS(SELECT 1 FROM brand_prefix_screenshots s WHERE s.prefix_id = brand_prefixes.id)"

const (
	deleteRejectedPrefix = "DELETE FROM brand_prefixes WHERE prefix = ? AND status = ?"

	insertBrandPrefix = "INSERT INTO brand_prefixes (org_id, prefix, status, registry, entity_id, header, " +
		"entity_name, decided_at) VALUES (?, ?, ?, ?, ?, ?, ?, " +
		"IF(? = 'APPROVED', CURRENT_TIMESTAMP, NULL))"

	insertBrandPrefixScreenshot = "INSERT INTO brand_prefix_screenshots (prefix_id, content_type, image) " +
		"VALUES (?, ?, ?)"
)

// Create inserts a request with the given status (PENDING, or APPROVED under
// auto-approval — decided_at is stamped then) and its DLT-portal screenshot,
// and returns the stored row. A REJECTED row holding the same prefix (any
// org) is removed in the same transaction (its screenshot cascades):
// rejection never holds a value.
func (s *BrandPrefixStore) Create(ctx *gofr.Context, p *models.BrandPrefix,
	shot *models.BrandPrefixScreenshot) (*models.BrandPrefix, error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}

	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx, deleteRejectedPrefix, p.Prefix, models.BrandPrefixStatusRejected); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx, insertBrandPrefix, p.OrgID, p.Prefix, p.Status, p.Registry,
		p.EntityID, p.Header, p.EntityName, p.Status)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, insertBrandPrefixScreenshot, id, shot.ContentType, shot.Image); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetByID(ctx, p.OrgID, id)
}

// GetByID fetches a prefix within the org; (nil, nil) when absent — a prefix
// from another org never resolves.
func (*BrandPrefixStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.BrandPrefix, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+brandPrefixColumns+" FROM brand_prefixes WHERE id = ? AND org_id = ?", id, orgID)

	return scanBrandPrefix(row)
}

// GetByPrefix fetches a prefix by value (case-insensitive), any status;
// (nil, nil) when absent. Deliberately not org-scoped: it backs the
// uniqueness check at request time and the prefix→org lookup on the
// redirect path.
func (*BrandPrefixStore) GetByPrefix(ctx *gofr.Context, prefix string) (*models.BrandPrefix, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+brandPrefixColumns+" FROM brand_prefixes WHERE prefix = ?", prefix)

	return scanBrandPrefix(row)
}

// IsHeld reports whether value (case-insensitive) is a PENDING, APPROVED or
// REVOKED prefix of any org — such a value can never become a short code.
func (*BrandPrefixStore) IsHeld(ctx *gofr.Context, value string) (bool, error) {
	var one int

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT 1 FROM brand_prefixes WHERE prefix = ? AND status <> ? LIMIT 1",
		value, models.BrandPrefixStatusRejected).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// ListByOrg returns all of the org's prefixes, oldest first.
func (*BrandPrefixStore) ListByOrg(ctx *gofr.Context, orgID int64) ([]models.BrandPrefix, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+brandPrefixColumns+" FROM brand_prefixes WHERE org_id = ? ORDER BY id", orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	prefixes := []models.BrandPrefix{}

	for rows.Next() {
		p, err := scanBrandPrefixRow(rows)
		if err != nil {
			return nil, err
		}

		prefixes = append(prefixes, *p)
	}

	return prefixes, rows.Err()
}

// CountHeldByOrg counts the org's non-REJECTED prefixes (the per-org cap).
func (*BrandPrefixStore) CountHeldByOrg(ctx *gofr.Context, orgID int64) (int, error) {
	var n int

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT COUNT(id) FROM brand_prefixes WHERE org_id = ? AND status <> ?",
		orgID, models.BrandPrefixStatusRejected).Scan(&n)

	return n, err
}

// PrimaryApprovedPrefix returns the org's primary APPROVED prefix, or "" when
// the org has none.
func (*BrandPrefixStore) PrimaryApprovedPrefix(ctx *gofr.Context, orgID int64) (string, error) {
	var prefix string

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT prefix FROM brand_prefixes WHERE org_id = ? AND is_primary = 1 AND status = ? LIMIT 1",
		orgID, models.BrandPrefixStatusApproved).Scan(&prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return prefix, nil
}

// SetPrimary makes this prefix the org's single primary in one transaction
// (the custom-domain swap): clear, then set — the SET requires APPROVED, and
// when it matches no row (false, nil) is returned and the swap rolls back.
func (*BrandPrefixStore) SetPrimary(ctx *gofr.Context, orgID, id int64) (bool, error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx,
		"UPDATE brand_prefixes SET is_primary = 0 WHERE org_id = ?", orgID); err != nil {
		return false, err
	}

	res, err := tx.ExecContext(ctx,
		"UPDATE brand_prefixes SET is_primary = 1 WHERE id = ? AND org_id = ? AND status = ?",
		id, orgID, models.BrandPrefixStatusApproved)
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

// Delete removes a prefix, org-scoped, unless it is REVOKED (a revoked value
// stays held until an operator releases it). Reports whether a row was
// deleted.
func (*BrandPrefixStore) Delete(ctx *gofr.Context, orgID, id int64) (bool, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"DELETE FROM brand_prefixes WHERE id = ? AND org_id = ? AND status <> ?",
		id, orgID, models.BrandPrefixStatusRevoked)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

func scanBrandPrefixRow(row rowScanner) (*models.BrandPrefix, error) {
	var p models.BrandPrefix

	err := row.Scan(&p.ID, &p.OrgID, &p.Prefix, &p.Status, &p.IsPrimary, &p.Registry, &p.EntityID,
		&p.Header, &p.EntityName, &p.DecisionReason, &p.DecidedAt, &p.CreatedAt, &p.HasScreenshot)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

func scanBrandPrefix(row *sql.Row) (*models.BrandPrefix, error) {
	p, err := scanBrandPrefixRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return p, nil
}
