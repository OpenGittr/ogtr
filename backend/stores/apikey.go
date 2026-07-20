package stores

import (
	"context"
	"database/sql"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// APIKeyStore reads and writes the api_keys table. key_hash is write-only:
// it is bound on insert and matched in GetByHash's WHERE clause, but never
// selected — no key material beyond the display hint leaves this layer.
type APIKeyStore struct{}

// NewAPIKeyStore builds an APIKeyStore.
func NewAPIKeyStore() *APIKeyStore { return &APIKeyStore{} }

const apiKeyColumns = "id, org_id, name, key_hint, status, created_at, last_used_at"

// Create inserts an API key and returns the stored row (without key material).
func (s *APIKeyStore) Create(ctx *gofr.Context, orgID int64, name, keyHash, keyHint string) (*models.APIKey, error) {
	res, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO api_keys (org_id, name, key_hash, key_hint) VALUES (?, ?, ?, ?)",
		orgID, name, keyHash, keyHint)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, orgID, id)
}

// GetByID fetches a key within the org; (nil, nil) when absent — a key from
// another org never resolves.
func (*APIKeyStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.APIKey, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+apiKeyColumns+" FROM api_keys WHERE id = ? AND org_id = ?", id, orgID)

	return scanAPIKey(row)
}

// GetByHash fetches a key by its SHA-256 hex digest; (nil, nil) when absent.
// This is the authentication lookup — deliberately not org-scoped, since the
// org context is derived FROM the key.
func (*APIKeyStore) GetByHash(ctx *gofr.Context, keyHash string) (*models.APIKey, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+apiKeyColumns+" FROM api_keys WHERE key_hash = ?", keyHash)

	return scanAPIKey(row)
}

// List returns all of the org's keys, newest first (disabled ones included —
// keys are never hard-deleted).
func (*APIKeyStore) List(ctx *gofr.Context, orgID int64) ([]models.APIKey, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+apiKeyColumns+" FROM api_keys WHERE org_id = ? ORDER BY created_at DESC, id DESC", orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	keys := []models.APIKey{}

	for rows.Next() {
		k, err := scanAPIKeyRow(rows)
		if err != nil {
			return nil, err
		}

		keys = append(keys, *k)
	}

	return keys, rows.Err()
}

// Disable sets a key's status to DISABLED, org-scoped.
func (*APIKeyStore) Disable(ctx *gofr.Context, orgID, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE api_keys SET status = ? WHERE id = ? AND org_id = ?",
		models.APIKeyStatusDisabled, id, orgID)

	return err
}

// TouchLastUsed stamps last_used_at. It is called fire-and-forget after a
// successful key authentication, typically from a goroutine that outlives the
// request — WithoutCancel keeps the UPDATE alive past the response.
func (*APIKeyStore) TouchLastUsed(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(context.WithoutCancel(ctx),
		"UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", id)

	return err
}

func scanAPIKeyRow(row rowScanner) (*models.APIKey, error) {
	var k models.APIKey

	err := row.Scan(&k.ID, &k.OrgID, &k.Name, &k.KeyHint, &k.Status, &k.CreatedAt, &k.LastUsedAt)
	if err != nil {
		return nil, err
	}

	return &k, nil
}

func scanAPIKey(row *sql.Row) (*models.APIKey, error) {
	k, err := scanAPIKeyRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return k, nil
}
