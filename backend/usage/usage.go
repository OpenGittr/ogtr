// Package usage meters per-org resource usage: cheap, read-only counters
// over the application's own tables.
//
// It exists for the limits seam (backend/limits): a deployment's
// limits.Policy implementation is constructed with a Reader and consults
// these counters inside its checks — the composition pattern documented in
// ARCHITECTURE.md §8 "Extension seam: LimitsPolicy". The open core itself
// uses only EventsThisMonth (the stats service's viewable-events gate);
// everything else is provided so policy implementations do not each reinvent
// the same queries.
//
// Counting conventions:
//   - "This month" is the current calendar month in UTC (from the 1st,
//     00:00:00 UTC). Month boundaries are computed in Go and bound as a
//     query parameter, so they are deterministic and testable.
//   - Events are click rows: every resolution records one (FEATURES.md
//     INV-4), so the event count is exactly the click count.
//   - Counters read live tables; nothing is cached. Every query is a single
//     indexed COUNT.
package usage

import (
	"time"

	"gofr.dev/pkg/gofr"
)

// Reader exposes per-org usage counters. limits.Policy implementations
// receive a Reader at construction; stores.* satisfy their own data needs —
// this interface is the policy-facing surface only.
type Reader interface {
	// EventsThisMonth counts the org's recorded clicks in the current
	// calendar month (UTC).
	EventsThisMonth(ctx *gofr.Context, orgID int64) (int64, error)

	// LinksCreatedThisMonth counts links the org created in the current
	// calendar month (UTC).
	LinksCreatedThisMonth(ctx *gofr.Context, orgID int64) (int64, error)

	// DomainsCount counts the org's registered custom domains (any status).
	DomainsCount(ctx *gofr.Context, orgID int64) (int64, error)

	// MembersCount counts the org's members.
	MembersCount(ctx *gofr.Context, orgID int64) (int64, error)

	// APIKeysActiveCount counts the org's ENABLED developer API keys
	// (disabled keys are kept forever and never count).
	APIKeysActiveCount(ctx *gofr.Context, orgID int64) (int64, error)
}

// Query shapes (constants so tests assert the exact SQL). All are org-scoped
// (FEATURES.md INV-6) and parameterized.
const (
	// eventsThisMonthQuery is served by the clicks (org_id, ts) index — the
	// (org_id, link_id, ts) analytics index cannot satisfy an org-wide ts
	// range because link_id sits between the two columns.
	eventsThisMonthQuery = "SELECT COUNT(id) FROM clicks WHERE org_id = ? AND ts >= ?"

	linksCreatedThisMonthQuery = "SELECT COUNT(id) FROM links WHERE org_id = ? AND created_at >= ?"

	domainsCountQuery = "SELECT COUNT(id) FROM domains WHERE org_id = ?"

	membersCountQuery = "SELECT COUNT(*) FROM org_members WHERE org_id = ?"

	apiKeysActiveCountQuery = "SELECT COUNT(id) FROM api_keys WHERE org_id = ? AND status = 'ENABLED'"
)

// nowFn supplies the clock; a package variable so month-boundary math is
// unit-testable.
var nowFn = time.Now

// monthStart returns the first instant of the current calendar month (UTC)
// in the YYYY-MM-DD form the queries bind.
func monthStart() string {
	now := nowFn().UTC()

	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// Store implements Reader over the application database.
type Store struct{}

// NewStore builds a Store.
func NewStore() *Store { return &Store{} }

// Compile-time interface check.
var _ Reader = (*Store)(nil)

// EventsThisMonth implements Reader.
func (*Store) EventsThisMonth(ctx *gofr.Context, orgID int64) (int64, error) {
	return count(ctx, eventsThisMonthQuery, orgID, monthStart())
}

// LinksCreatedThisMonth implements Reader.
func (*Store) LinksCreatedThisMonth(ctx *gofr.Context, orgID int64) (int64, error) {
	return count(ctx, linksCreatedThisMonthQuery, orgID, monthStart())
}

// DomainsCount implements Reader.
func (*Store) DomainsCount(ctx *gofr.Context, orgID int64) (int64, error) {
	return count(ctx, domainsCountQuery, orgID)
}

// MembersCount implements Reader.
func (*Store) MembersCount(ctx *gofr.Context, orgID int64) (int64, error) {
	return count(ctx, membersCountQuery, orgID)
}

// APIKeysActiveCount implements Reader.
func (*Store) APIKeysActiveCount(ctx *gofr.Context, orgID int64) (int64, error) {
	return count(ctx, apiKeysActiveCountQuery, orgID)
}

func count(ctx *gofr.Context, query string, args ...any) (int64, error) {
	var n int64

	if err := ctx.SQL.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}

	return n, nil
}
