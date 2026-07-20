package stores

import (
	"database/sql"
	"encoding/json"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// LinkStore reads and writes the links table. Every query is org-scoped
// except GetByCode and CodeExists: resolution is public and the code column
// is a single deployment-wide namespace (ARCHITECTURE.md §2).
type LinkStore struct{}

// NewLinkStore builds a LinkStore.
func NewLinkStore() *LinkStore { return &LinkStore{} }

const linkColumns = "id, org_id, user_id, api_key_id, code, destination_url, type, status, " +
	"utm_source, utm_medium, utm_campaign, deeplink_config, visits, last_visit_at, created_at"

// visibleFilter hides other users' PRIVATE links (FEATURES.md §1.1); the
// caller binds org_id then the viewer's user_id.
const visibleFilter = "org_id = ? AND (type = 'PUBLIC' OR user_id = ?)"

const insertLink = "INSERT INTO links (org_id, user_id, api_key_id, code, destination_url, type, " +
	"utm_source, utm_medium, utm_campaign) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"

// Create inserts a link and returns the stored row.
func (s *LinkStore) Create(ctx *gofr.Context, l *models.Link) (*models.Link, error) {
	res, err := ctx.SQL.ExecContext(ctx, insertLink,
		l.OrgID, l.UserID, l.APIKeyID, l.Code, l.DestinationURL, l.Type,
		l.UTMSource, l.UTMMedium, l.UTMCampaign)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, l.OrgID, id)
}

// GetByID fetches a link within the org; (nil, nil) when absent.
func (*LinkStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.Link, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+linkColumns+" FROM links WHERE id = ? AND org_id = ?", id, orgID)

	return scanLink(row)
}

// GetByCode fetches a link by its short code; (nil, nil) when absent. This is
// the public resolution lookup — deliberately not org-scoped, since the code
// namespace is deployment-wide.
func (*LinkStore) GetByCode(ctx *gofr.Context, code string) (*models.Link, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+linkColumns+" FROM links WHERE code = ?", code)

	return scanLink(row)
}

// FindByDestination returns the viewer-visible link with this exact
// destination inside the org, for per-org dedupe. A missing match is
// (nil, nil), not an error — "no match" is a normal outcome, never a failure.
func (*LinkStore) FindByDestination(ctx *gofr.Context, orgID, viewerID int64, destinationURL string) (*models.Link, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+linkColumns+" FROM links WHERE "+visibleFilter+
			" AND destination_url = ? ORDER BY id LIMIT 1",
		orgID, viewerID, destinationURL)

	return scanLink(row)
}

// CodeExists reports whether a code (generated or alias) is already taken
// anywhere in the deployment — the single uniqueness namespace (INV-5).
func (*LinkStore) CodeExists(ctx *gofr.Context, code string) (bool, error) {
	var one int

	err := ctx.SQL.QueryRowContext(ctx, "SELECT 1 FROM links WHERE code = ?", code).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// List returns a page of the org's links visible to the viewer, newest first.
func (*LinkStore) List(ctx *gofr.Context, orgID, viewerID int64, limit, offset int) ([]models.Link, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+linkColumns+" FROM links WHERE "+visibleFilter+
			" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
		orgID, viewerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	links := []models.Link{}

	for rows.Next() {
		l, err := scanLinkRow(rows)
		if err != nil {
			return nil, err
		}

		links = append(links, *l)
	}

	return links, rows.Err()
}

// Count returns the number of the org's links visible to the viewer.
func (*LinkStore) Count(ctx *gofr.Context, orgID, viewerID int64) (int64, error) {
	var n int64

	err := ctx.SQL.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM links WHERE "+visibleFilter, orgID, viewerID).Scan(&n)
	if err != nil {
		return 0, err
	}

	return n, nil
}

// UpdateCode replaces a link's short code (custom alias). The old code stops
// resolving — documented behavior (ARCHITECTURE.md §2).
func (*LinkStore) UpdateCode(ctx *gofr.Context, orgID, id int64, code string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE links SET code = ? WHERE id = ? AND org_id = ?", code, id, orgID)

	return err
}

// UpdateDeeplink replaces a link's deep-link config; nil (or an empty config)
// clears the column to NULL. Only the owner-facing management API calls this —
// the resolution path never writes link config (FEATURES.md INV-3).
func (*LinkStore) UpdateDeeplink(ctx *gofr.Context, orgID, id int64, cfg *models.DeeplinkConfig) error {
	var value any

	if !cfg.Empty() {
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}

		// string, not []byte: the MySQL driver sends []byte with the binary
		// charset, which JSON columns reject (error 3144).
		value = string(raw)
	}

	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE links SET deeplink_config = ? WHERE id = ? AND org_id = ?", value, id, orgID)

	return err
}

// UpdateDestination repoints a link at a new destination URL, replacing the
// as-created UTM columns with the edit's values. Only the owner-facing
// management API calls this (PATCH /api/v1/links/{id}); permission checks
// live in the service layer.
func (*LinkStore) UpdateDestination(ctx *gofr.Context, orgID, id int64, destinationURL string,
	utmSource, utmMedium, utmCampaign *string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE links SET destination_url = ?, utm_source = ?, utm_medium = ?, utm_campaign = ? "+
			"WHERE id = ? AND org_id = ?",
		destinationURL, utmSource, utmMedium, utmCampaign, id, orgID)

	return err
}

const insertLinkEdit = "INSERT INTO link_edits (org_id, link_id, user_id, old_url, new_url) " +
	"VALUES (?, ?, ?, ?, ?)"

// InsertEdit writes one destination-edit audit row (link_edits); called on
// every successful PATCH /api/v1/links/{id}.
func (*LinkStore) InsertEdit(ctx *gofr.Context, e *models.LinkEdit) error {
	_, err := ctx.SQL.ExecContext(ctx, insertLinkEdit,
		e.OrgID, e.LinkID, e.UserID, e.OldURL, e.NewURL)

	return err
}

// SetStatusByID sets a link's status (ACTIVE | DISABLED_ABUSE). Called only
// by the periodic destination re-scan — a system job with no auth context,
// which is why this one write is not org-scoped (the id comes from the
// re-scan's own deployment-wide listing, never from a client).
func (*LinkStore) SetStatusByID(ctx *gofr.Context, id int64, status string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE links SET status = ? WHERE id = ?", status, id)

	return err
}

// ListActiveClickedSince pages through ACTIVE links whose last visit is at
// or after since, ordered by id with an id cursor — the periodic re-scan's
// work queue (recently clicked links are the ones exposing visitors).
func (*LinkStore) ListActiveClickedSince(ctx *gofr.Context, since string, afterID int64, limit int) ([]models.Link, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+linkColumns+" FROM links WHERE status = 'ACTIVE' AND last_visit_at >= ? AND id > ? "+
			"ORDER BY id LIMIT ?",
		since, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	links := []models.Link{}

	for rows.Next() {
		l, err := scanLinkRow(rows)
		if err != nil {
			return nil, err
		}

		links = append(links, *l)
	}

	return links, rows.Err()
}

// RecordVisit bumps the per-link counters on resolution.
func (*LinkStore) RecordVisit(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"UPDATE links SET visits = visits + 1, last_visit_at = CURRENT_TIMESTAMP WHERE id = ?", id)

	return err
}

// rowScanner covers *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLinkRow(row rowScanner) (*models.Link, error) {
	var (
		l           models.Link
		rawDeeplink []byte
	)

	err := row.Scan(&l.ID, &l.OrgID, &l.UserID, &l.APIKeyID, &l.Code, &l.DestinationURL, &l.Type, &l.Status,
		&l.UTMSource, &l.UTMMedium, &l.UTMCampaign, &rawDeeplink, &l.Visits, &l.LastVisitAt, &l.CreatedAt)
	if err != nil {
		return nil, err
	}

	if len(rawDeeplink) > 0 {
		if err := json.Unmarshal(rawDeeplink, &l.Deeplink); err != nil {
			return nil, err
		}
	}

	return &l, nil
}

func scanLink(row *sql.Row) (*models.Link, error) {
	l, err := scanLinkRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return l, nil
}
