package usage

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

var errDB = errors.New("db down")

// newTestCtx builds a gofr context backed by the framework's sqlmock-based
// mock container (same pattern as the stores tests).
func newTestCtx(t *testing.T) (*gofr.Context, *container.Mocks) {
	t.Helper()

	mockContainer, mocks := container.NewMockContainer(t)

	return &gofr.Context{Context: context.Background(), Container: mockContainer}, mocks
}

// fixNow pins the usage clock and restores it after the test.
func fixNow(t *testing.T, iso string) {
	t.Helper()

	fixed, err := time.Parse(time.RFC3339, iso)
	require.NoError(t, err)

	prev := nowFn
	nowFn = func() time.Time { return fixed }

	t.Cleanup(func() { nowFn = prev })
}

// Month-boundary math: the current-month window starts on the 1st, 00:00 UTC,
// whatever the local zone of the clock says.
func TestMonthStart(t *testing.T) {
	tests := []struct {
		desc string
		now  string
		want string
	}{
		{desc: "mid-month", now: "2026-07-21T10:30:00Z", want: "2026-07-01"},
		{desc: "first instant of a month", now: "2026-07-01T00:00:00Z", want: "2026-07-01"},
		{desc: "last instant of a month", now: "2026-07-31T23:59:59Z", want: "2026-07-01"},
		{desc: "january (year boundary)", now: "2026-01-01T00:00:00Z", want: "2026-01-01"},
		// 2026-08-01 05:00 +05:30 is 2026-07-31 23:30 UTC: still July in the
		// UTC convention the counters are defined in.
		{desc: "local-zone month ahead of UTC", now: "2026-08-01T05:00:00+05:30", want: "2026-07-01"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			fixNow(t, tc.now)

			assert.Equal(t, tc.want, monthStart())
		})
	}
}

// Query shapes: each counter is one org-scoped, parameterized COUNT with the
// exact SQL pinned by the expectation.
func TestStore_Counters(t *testing.T) {
	fixNow(t, "2026-07-21T10:30:00Z")

	const org = int64(3)

	store := NewStore()

	tests := []struct {
		desc  string
		query string
		args  []driver.Value
		call  func(ctx *gofr.Context) (int64, error)
	}{
		{
			desc:  "events this month binds the UTC month start",
			query: eventsThisMonthQuery,
			args:  []driver.Value{org, "2026-07-01"},
			call:  func(ctx *gofr.Context) (int64, error) { return store.EventsThisMonth(ctx, org) },
		},
		{
			desc:  "links created this month binds the UTC month start",
			query: linksCreatedThisMonthQuery,
			args:  []driver.Value{org, "2026-07-01"},
			call:  func(ctx *gofr.Context) (int64, error) { return store.LinksCreatedThisMonth(ctx, org) },
		},
		{
			desc:  "domains count",
			query: domainsCountQuery,
			args:  []driver.Value{org},
			call:  func(ctx *gofr.Context) (int64, error) { return store.DomainsCount(ctx, org) },
		},
		{
			desc:  "members count",
			query: membersCountQuery,
			args:  []driver.Value{org},
			call:  func(ctx *gofr.Context) (int64, error) { return store.MembersCount(ctx, org) },
		},
		{
			desc:  "active api keys count",
			query: apiKeysActiveCountQuery,
			args:  []driver.Value{org},
			call:  func(ctx *gofr.Context) (int64, error) { return store.APIKeysActiveCount(ctx, org) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)

			mocks.SQL.ExpectQuery(tc.query).WithArgs(tc.args...).
				WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(42))

			got, err := tc.call(ctx)

			require.NoError(t, err)
			assert.Equal(t, int64(42), got)
		})

		t.Run(tc.desc+" db error", func(t *testing.T) {
			ctx, mocks := newTestCtx(t)

			mocks.SQL.ExpectQuery(tc.query).WithArgs(tc.args...).WillReturnError(errDB)

			got, err := tc.call(ctx)

			require.ErrorIs(t, err, errDB)
			assert.Zero(t, got)
		})
	}
}

// The month boundary flips the events window: a count on July 31 and one on
// August 1 bind different month starts — the calendar month resets the
// viewable-events meter.
func TestStore_EventsThisMonth_MonthBoundary(t *testing.T) {
	store := NewStore()

	t.Run("july 31", func(t *testing.T) {
		fixNow(t, "2026-07-31T23:59:59Z")
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectQuery(eventsThisMonthQuery).WithArgs(int64(3), "2026-07-01").
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(999))

		got, err := store.EventsThisMonth(ctx, 3)

		require.NoError(t, err)
		assert.Equal(t, int64(999), got)
	})

	t.Run("august 1", func(t *testing.T) {
		fixNow(t, "2026-08-01T00:00:00Z")
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectQuery(eventsThisMonthQuery).WithArgs(int64(3), "2026-08-01").
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))

		got, err := store.EventsThisMonth(ctx, 3)

		require.NoError(t, err)
		assert.Zero(t, got, "a new calendar month starts a fresh events window")
	})
}
