package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
)

var errStatsSvc = errors.New("stats service down")

func newStatsHandler(t *testing.T) (*StatsHandler, *MockStatsService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockStatsService(ctrl)

	return NewStatsHandler(svc), svc
}

func TestStatsHandler_LinkReport(t *testing.T) {
	// The fixture carries a location breakdown (incl. the Unknown bucket) so
	// the passthrough assertion pins that the report serves it unchanged.
	report := &models.LinkStatsReport{
		TotalClicks: 30,
		TotalBreakdowns: models.TotalBreakdowns{Location: models.LocationTotals{
			Countries: []models.DimCount{{Value: "India", Clicks: 28}, {Value: "Unknown", Clicks: 2}},
			Regions:   []models.DimCount{{Value: "Karnataka", Clicks: 28}},
			Cities:    []models.DimCount{{Value: "Bengaluru", Clicks: 28}},
		}},
	}

	tests := []struct {
		desc    string
		query   string
		vars    map[string]string
		claims  *auth.SessionClaims
		setup   func(svc *MockStatsService)
		wantErr bool
	}{
		{
			desc:   "defaults: empty dates, deeplink false",
			vars:   map[string]string{"id": "9"},
			claims: orgOwnerClaims(),
			setup: func(svc *MockStatsService) {
				svc.EXPECT().LinkReport(gomock.Any(), int64(3), int64(7), int64(9), "", "", false).
					Return(report, nil)
			},
		},
		{
			desc:   "explicit range and deeplink flag",
			query:  "?from=2026-06-01&to=2026-06-30&deeplink=true",
			vars:   map[string]string{"id": "9"},
			claims: orgOwnerClaims(),
			setup: func(svc *MockStatsService) {
				svc.EXPECT().LinkReport(gomock.Any(), int64(3), int64(7), int64(9),
					"2026-06-01", "2026-06-30", true).Return(report, nil)
			},
		},
		{
			desc:    "garbage deeplink flag",
			query:   "?deeplink=maybe",
			vars:    map[string]string{"id": "9"},
			claims:  orgOwnerClaims(),
			wantErr: true,
		},
		{
			desc:    "non-numeric link id",
			vars:    map[string]string{"id": "abc"},
			claims:  orgOwnerClaims(),
			wantErr: true,
		},
		{
			desc:    "org-less token rejected",
			vars:    map[string]string{"id": "9"},
			claims:  orglessClaims(),
			wantErr: true,
		},
		{
			desc:    "unauthenticated",
			vars:    map[string]string{"id": "9"},
			claims:  nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newStatsHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/9/stats"+tc.query, "", tc.claims, tc.vars)

			got, err := h.LinkReport(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, report, got)
		})
	}
}

// TestStatsHandler_LinkReport_UntypedNilOnError pins the gofr 206 regression:
// on a service error the handler's first return must be exactly nil, not a
// typed nil pointer.
func TestStatsHandler_LinkReport_UntypedNilOnError(t *testing.T) {
	h, svc := newStatsHandler(t)
	svc.EXPECT().LinkReport(gomock.Any(), int64(3), int64(7), int64(9), "", "", false).
		Return(nil, errStatsSvc)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/9/stats", "", orgOwnerClaims(),
		map[string]string{"id": "9"})

	got, err := h.LinkReport(ctx)

	require.Error(t, err)
	assert.Nil(t, got)
}

func TestStatsHandler_UniqueClicks(t *testing.T) {
	result := &models.UniqueClicksResult{UniqueClicks: 2}

	tests := []struct {
		desc    string
		query   string
		claims  *auth.SessionClaims
		setup   func(svc *MockStatsService)
		wantErr bool
	}{
		{
			desc:   "csv ids parsed",
			query:  "?link_ids=1,2,3",
			claims: orgOwnerClaims(),
			setup: func(svc *MockStatsService) {
				svc.EXPECT().UniqueClicks(gomock.Any(), int64(3), []int64{1, 2, 3}).Return(result, nil)
			},
		},
		{
			desc:   "spaces around ids tolerated",
			query:  "?link_ids=1,%202",
			claims: orgOwnerClaims(),
			setup: func(svc *MockStatsService) {
				svc.EXPECT().UniqueClicks(gomock.Any(), int64(3), []int64{1, 2}).Return(result, nil)
			},
		},
		{desc: "missing link_ids", query: "", claims: orgOwnerClaims(), wantErr: true},
		{desc: "empty link_ids", query: "?link_ids=", claims: orgOwnerClaims(), wantErr: true},
		{desc: "garbage id", query: "?link_ids=1,x", claims: orgOwnerClaims(), wantErr: true},
		{desc: "trailing comma", query: "?link_ids=1,2,", claims: orgOwnerClaims(), wantErr: true},
		{desc: "non-positive id", query: "?link_ids=0", claims: orgOwnerClaims(), wantErr: true},
		{desc: "org-less token rejected", query: "?link_ids=1", claims: orglessClaims(), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newStatsHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/unique-clicks"+tc.query, "", tc.claims, nil)

			got, err := h.UniqueClicks(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, result, got)
		})
	}
}

func TestStatsHandler_Tags(t *testing.T) {
	t.Run("tags listed", func(t *testing.T) {
		h, svc := newStatsHandler(t)
		svc.EXPECT().Tags(gomock.Any(), int64(3)).Return([]string{"promo"}, nil)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/tags", "", orgOwnerClaims(), nil)

		got, err := h.Tags(ctx)

		require.NoError(t, err)
		assert.Equal(t, []string{"promo"}, got)
	})

	t.Run("untyped nil on service error (gofr 206 regression)", func(t *testing.T) {
		h, svc := newStatsHandler(t)
		svc.EXPECT().Tags(gomock.Any(), int64(3)).Return(nil, errStatsSvc)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/tags", "", orgOwnerClaims(), nil)

		got, err := h.Tags(ctx)

		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("org-less token rejected", func(t *testing.T) {
		h, _ := newStatsHandler(t)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/tags", "", orglessClaims(), nil)

		_, err := h.Tags(ctx)

		require.Error(t, err)
	})
}

func TestStatsHandler_UTM(t *testing.T) {
	t.Run("analysis returned", func(t *testing.T) {
		h, svc := newStatsHandler(t)

		analysis := &models.UTMAnalysis{
			SourceAnalysis:   []models.UTMCount{{UTMValue: "google", LinkID: 9, Clicks: 5}},
			MediumAnalysis:   []models.UTMCount{},
			CampaignAnalysis: []models.UTMCount{},
		}
		svc.EXPECT().UTMAnalysis(gomock.Any(), int64(3), int64(7)).Return(analysis, nil)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/utm", "", orgOwnerClaims(), nil)

		got, err := h.UTM(ctx)

		require.NoError(t, err)
		assert.Equal(t, analysis, got)
	})

	t.Run("untyped nil on service error (gofr 206 regression)", func(t *testing.T) {
		h, svc := newStatsHandler(t)
		svc.EXPECT().UTMAnalysis(gomock.Any(), int64(3), int64(7)).Return(nil, errStatsSvc)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/utm", "", orgOwnerClaims(), nil)

		got, err := h.UTM(ctx)

		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		h, _ := newStatsHandler(t)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/stats/utm", "", nil, nil)

		_, err := h.UTM(ctx)

		require.Error(t, err)
	})
}
