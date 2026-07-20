package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// InviteStore reads and writes the invites table.
type InviteStore struct{}

// NewInviteStore builds an InviteStore.
func NewInviteStore() *InviteStore { return &InviteStore{} }

const inviteColumns = "id, org_id, email, invited_by, status, created_at"

// Create inserts a pending invite and returns it.
func (*InviteStore) Create(ctx *gofr.Context, orgID int64, email string, invitedBy int64) (*models.Invite, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO invites (org_id, email, invited_by, status) VALUES (?, ?, ?, ?)",
		orgID, email, invitedBy, models.InviteStatusPending)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+inviteColumns+" FROM invites WHERE id = ? AND org_id = ?", id, orgID)

	return scanInvite(row)
}

// GetByID fetches an invite within the org; (nil, nil) when absent.
func (*InviteStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.Invite, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+inviteColumns+" FROM invites WHERE id = ? AND org_id = ?", id, orgID)

	return scanInvite(row)
}

// ListPending lists the org's pending invites.
func (*InviteStore) ListPending(ctx *gofr.Context, orgID int64) ([]models.Invite, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+inviteColumns+" FROM invites WHERE org_id = ? AND status = ? ORDER BY created_at, id",
		orgID, models.InviteStatusPending)
	if err != nil {
		return nil, err
	}

	return collectInvites(rows)
}

// HasPending reports whether the org already has a pending invite for the email.
func (*InviteStore) HasPending(ctx *gofr.Context, orgID int64, email string) (bool, error) {
	var one int

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT 1 FROM invites WHERE org_id = ? AND email = ? AND status = ? LIMIT 1",
		orgID, email, models.InviteStatusPending).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// PendingForEmail lists pending invites for an email across orgs. Runs at
// login (before an org context exists) to auto-convert invites to membership.
func (*InviteStore) PendingForEmail(ctx *gofr.Context, email string) ([]models.Invite, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+inviteColumns+" FROM invites WHERE email = ? AND status = ? ORDER BY created_at, id",
		email, models.InviteStatusPending)
	if err != nil {
		return nil, err
	}

	return collectInvites(rows)
}

// SetStatus updates an invite's status (ACCEPTED on login, REVOKED by owner).
func (*InviteStore) SetStatus(ctx *gofr.Context, id int64, status string) error {
	_, err := ctx.SQL.ExecContext(ctx, "UPDATE invites SET status = ? WHERE id = ?", status, id)

	return err
}

func scanInvite(row *sql.Row) (*models.Invite, error) {
	var inv models.Invite

	err := row.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.InvitedBy, &inv.Status, &inv.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &inv, nil
}

func collectInvites(rows *sql.Rows) ([]models.Invite, error) {
	defer func() { _ = rows.Close() }()

	invites := []models.Invite{}

	for rows.Next() {
		var inv models.Invite

		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.InvitedBy, &inv.Status, &inv.CreatedAt); err != nil {
			return nil, err
		}

		invites = append(invites, inv)
	}

	return invites, rows.Err()
}
