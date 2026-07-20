package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// MemberStore reads and writes the org_members table.
type MemberStore struct{}

// NewMemberStore builds a MemberStore.
func NewMemberStore() *MemberStore { return &MemberStore{} }

// GetRole returns the user's role in the org; ("", nil) when not a member.
func (*MemberStore) GetRole(ctx *gofr.Context, orgID, userID int64) (string, error) {
	var role string

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT role FROM org_members WHERE org_id = ? AND user_id = ?", orgID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return role, nil
}

// Add inserts a membership.
func (*MemberStore) Add(ctx *gofr.Context, orgID, userID int64, role string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)", orgID, userID, role)

	return err
}

// Remove deletes a membership.
func (*MemberStore) Remove(ctx *gofr.Context, orgID, userID int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"DELETE FROM org_members WHERE org_id = ? AND user_id = ?", orgID, userID)

	return err
}

// CountOwners returns the number of OWNER members in the org (guards the
// "cannot remove the last owner" rule).
func (*MemberStore) CountOwners(ctx *gofr.Context, orgID int64) (int, error) {
	var n int

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM org_members WHERE org_id = ? AND role = ?", orgID, models.RoleOwner).Scan(&n)
	if err != nil {
		return 0, err
	}

	return n, nil
}

// ListOrgsForUser lists all orgs a user belongs to, with their role, ordered
// by join time (identity-level query — runs before an org context exists).
func (*MemberStore) ListOrgsForUser(ctx *gofr.Context, userID int64) ([]models.OrgMembership, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT o.id, o.name, o.slug, m.role
		 FROM org_members m JOIN orgs o ON o.id = m.org_id
		 WHERE m.user_id = ? ORDER BY m.created_at, o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	memberships := []models.OrgMembership{}

	for rows.Next() {
		var m models.OrgMembership

		if err := rows.Scan(&m.OrgID, &m.Name, &m.Slug, &m.Role); err != nil {
			return nil, err
		}

		memberships = append(memberships, m)
	}

	return memberships, rows.Err()
}

// ListMembers lists an org's members with user details.
func (*MemberStore) ListMembers(ctx *gofr.Context, orgID int64) ([]models.Member, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT u.id, u.name, u.email, m.role, m.created_at
		 FROM org_members m JOIN users u ON u.id = m.user_id
		 WHERE m.org_id = ? ORDER BY m.created_at, u.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	members := []models.Member{}

	for rows.Next() {
		var m models.Member

		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}

		members = append(members, m)
	}

	return members, rows.Err()
}
