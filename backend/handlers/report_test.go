package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/visitor"
)

func newReportHandler(t *testing.T) (*ReportHandler, *MockReportService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockReportService(ctrl)

	return NewReportHandler(svc), svc
}

// withVisitorIP stashes a visitor env (as the middleware would) carrying the
// reporter IP.
func withVisitorIP(ctx *gofr.Context, ip string) *gofr.Context {
	ctx.Context = visitor.ContextWithEnv(ctx.Context, visitor.Env{IP: ip})

	return ctx
}

func TestReportHandler_Create(t *testing.T) {
	h, svc := newReportHandler(t)

	ctx := withVisitorIP(newTestCtx(t, http.MethodPost, "/api/v1/report",
		`{"code":"abc1234","reason":"phishing","reporter_contact":"r@example.com"}`, nil, nil), "9.9.9.9")

	svc.EXPECT().Create(gomock.Any(),
		services.ReportInput{Code: "abc1234", Reason: "phishing", ReporterContact: "r@example.com"},
		"9.9.9.9").
		Return(&models.AbuseReport{ID: 17}, nil)

	got, err := h.Create(ctx)

	require.NoError(t, err)
	receipt, ok := got.(reportReceipt)
	require.True(t, ok)
	assert.Equal(t, int64(17), receipt.ID)
	assert.Equal(t, "received", receipt.Status)
}

func TestReportHandler_Create_ServiceErrorsPassThrough(t *testing.T) {
	h, svc := newReportHandler(t)

	ctx := withVisitorIP(newTestCtx(t, http.MethodPost, "/api/v1/report",
		`{"code":"abc1234","reason":"x"}`, nil, nil), "9.9.9.9")

	svc.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, apierrors.TooManyRequests("too many reports"))

	_, err := h.Create(ctx)

	require.Error(t, err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode())
}

func TestReportHandler_Create_BadBodyIs400(t *testing.T) {
	h, _ := newReportHandler(t)

	ctx := withVisitorIP(newTestCtx(t, http.MethodPost, "/api/v1/report", `{not json`, nil, nil), "9.9.9.9")

	_, err := h.Create(ctx)

	require.Error(t, err)
}
