package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// OrgStore reads and writes the orgs table.
type OrgStore struct{}

// NewOrgStore builds an OrgStore.
func NewOrgStore() *OrgStore { return &OrgStore{} }

const orgColumns = "id, name, slug, auto_join_domain, created_at"

// Create inserts an org and returns it.
func (s *OrgStore) Create(ctx *gofr.Context, name, slug string, autoJoinDomain *string) (*models.Org, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO orgs (name, slug, auto_join_domain) VALUES (?, ?, ?)", name, slug, autoJoinDomain)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, id)
}

// GetByID fetches an org; (nil, nil) when absent.
func (*OrgStore) GetByID(ctx *gofr.Context, id int64) (*models.Org, error) {
	row := ctx.SQL.QueryRowContext(ctx, "SELECT "+orgColumns+" FROM orgs WHERE id = ?", id)

	return scanOrg(row)
}

// SlugExists reports whether a slug is already taken.
func (*OrgStore) SlugExists(ctx *gofr.Context, slug string) (bool, error) {
	var one int

	err := ctx.SQL.QueryRowContext(ctx, "SELECT 1 FROM orgs WHERE slug = ?", slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// Update sets an org's name and auto_join_domain (nil clears the domain).
func (*OrgStore) Update(ctx *gofr.Context, id int64, name string, autoJoinDomain *string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE orgs SET name = ?, auto_join_domain = ? WHERE id = ?", name, autoJoinDomain, id)

	return err
}

// GetByAutoJoinDomain returns the first org (by id) whose auto_join_domain
// matches; (nil, nil) when none does. Used at login for domain auto-join.
func (*OrgStore) GetByAutoJoinDomain(ctx *gofr.Context, domain string) (*models.Org, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+orgColumns+" FROM orgs WHERE auto_join_domain = ? ORDER BY id LIMIT 1", domain)

	return scanOrg(row)
}

func scanOrg(row *sql.Row) (*models.Org, error) {
	var o models.Org

	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.AutoJoinDomain, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &o, nil
}
