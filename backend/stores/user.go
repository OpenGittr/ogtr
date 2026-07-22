// Package stores contains the data-access layer for backend. Every
// query against an org-scoped table filters by org_id (ARCHITECTURE.md §2);
// the users/orgs identity lookups in this package run before an org context
// exists (login, membership resolution) and are the documented exception.
package stores

import (
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// UserStore reads and writes the users table.
type UserStore struct{}

// NewUserStore builds a UserStore.
func NewUserStore() *UserStore { return &UserStore{} }

const userColumns = "id, name, email, status, created_at"

// GetByEmail fetches a user by email. A missing user is (nil, nil), not an
// error — "not found" is a normal outcome during login, never a failure.
func (*UserStore) GetByEmail(ctx *gofr.Context, email string) (*models.User, error) {
	row := ctx.SQL.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE email = ?", email)

	return scanUser(row)
}

// GetByID fetches a user by ID; (nil, nil) when absent.
func (*UserStore) GetByID(ctx *gofr.Context, id int64) (*models.User, error) {
	row := ctx.SQL.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id)

	return scanUser(row)
}

// Create inserts a user (JIT provisioning on first login) and returns it.
func (*UserStore) Create(ctx *gofr.Context, name, email string) (*models.User, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO users (name, email, status) VALUES (?, ?, ?)", name, email, models.UserStatusEnabled)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	row := ctx.SQL.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id)

	return scanUser(row)
}

// touchLastActiveQuery is the throttled activity touch: one conditional
// primary-key UPDATE, written only when the stored value is NULL or older
// than an hour, so the login/refresh hot paths never write more than once
// per user per hour and need no prior read.
const touchLastActiveQuery = "UPDATE users SET last_active_at = NOW() WHERE id = ? " +
	"AND (last_active_at IS NULL OR last_active_at < NOW() - INTERVAL 1 HOUR)"

// TouchLastActive records that the user was just seen (login, token
// refresh), throttled to at most one write per hour by the conditional
// UPDATE itself. A no-op match (recently touched) is success.
func (*UserStore) TouchLastActive(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx, touchLastActiveQuery, id)

	return err
}

func scanUser(row *sql.Row) (*models.User, error) {
	var u models.User

	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &u, nil
}
