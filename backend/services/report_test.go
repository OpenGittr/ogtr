package services

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
)

func newReportService(t *testing.T, limiter *ratelimit.SlidingWindow) (
	*ReportService, *MockLinkStore, *MockAbuseReportStore, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	reports := NewMockAbuseReportStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewReportService(links, reports, limiter), links, reports, ctx
}

func TestReportService_Create_WritesRow(t *testing.T) {
	svc, links, reports, ctx := newReportService(t, nil)

	links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://x.co"), nil)

	var inserted models.AbuseReport

	reports.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.AbuseReport) error {
			r.ID = 17
			inserted = *r

			return nil
		})

	got, err := svc.Create(ctx, ReportInput{
		Code:            " abc1234 ",
		Reason:          "  phishing page pretending to be a bank  ",
		ReporterContact: " reporter@example.com ",
	}, "9.9.9.9")

	require.NoError(t, err)
	assert.Equal(t, int64(17), got.ID)

	// org/link derived from the link row; fields trimmed; contact stored.
	assert.Equal(t, int64(3), inserted.OrgID)
	assert.Equal(t, int64(9), inserted.LinkID)
	assert.Equal(t, "abc1234", inserted.Code)
	assert.Equal(t, "phishing page pretending to be a bank", inserted.Reason)
	require.NotNil(t, inserted.ReporterContact)
	assert.Equal(t, "reporter@example.com", *inserted.ReporterContact)
}

func TestReportService_Create_Validation(t *testing.T) {
	tests := []struct {
		desc string
		in   ReportInput
	}{
		{desc: "missing code", in: ReportInput{Reason: "bad"}},
		{desc: "missing reason", in: ReportInput{Code: "abc1234"}},
		{desc: "reason over 140 chars", in: ReportInput{Code: "abc1234", Reason: strings.Repeat("x", 141)}},
		{desc: "contact too long", in: ReportInput{Code: "abc1234", Reason: "bad",
			ReporterContact: strings.Repeat("c", 256)}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, _, ctx := newReportService(t, nil)

			_, err := svc.Create(ctx, tc.in, "9.9.9.9")

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestReportService_Create_UnknownCodeIs404(t *testing.T) {
	svc, links, _, ctx := newReportService(t, nil)

	links.EXPECT().GetByCode(gomock.Any(), "unknown").Return(nil, nil)

	_, err := svc.Create(ctx, ReportInput{Code: "unknown", Reason: "bad"}, "9.9.9.9")

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestReportService_Create_RateLimited(t *testing.T) {
	svc, links, reports, ctx := newReportService(t, ratelimit.NewSlidingWindow(5, time.Minute))

	links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://x.co"), nil).Times(5)
	reports.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil).Times(5)

	in := ReportInput{Code: "abc1234", Reason: "spam"}

	for range 5 {
		_, err := svc.Create(ctx, in, "9.9.9.9")
		require.NoError(t, err)
	}

	// 6th report from the same IP inside the window: 429, no store calls.
	_, err := svc.Create(ctx, in, "9.9.9.9")
	require.Error(t, err)
	assertStatus(t, err, http.StatusTooManyRequests)

	// Another IP still reports fine.
	links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://x.co"), nil)
	reports.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil)

	_, err = svc.Create(ctx, in, "8.8.8.8")
	require.NoError(t, err)
}
