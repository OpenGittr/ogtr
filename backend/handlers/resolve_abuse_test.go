package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/services"
)

func TestResolveHandler_Redirect_DisabledLinkRenders410Page(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := NewMockResolveService(ctrl)
	h := NewResolveHandler(svc, NewMockAPIKeyService(ctrl), "", "sho.rt", "abuse@sho.rt")

	svc.EXPECT().Resolve(gomock.Any(), "abc1234", "", gomock.Any()).
		Return(nil, &services.DisabledLinkError{Code: "abc1234", AbuseContact: "abuse@sho.rt"})

	ctx := newTestCtx(t, http.MethodGet, "/abc1234", "", nil, map[string]string{"code": "abc1234"})

	got, err := h.Redirect(ctx)

	// HTML page as data, 410 error as status — gofr renders File+error as
	// "this body with the error's status code".
	require.Error(t, err)

	var disabled *services.DisabledLinkError
	require.ErrorAs(t, err, &disabled)
	assert.Equal(t, http.StatusGone, disabled.StatusCode())

	file, ok := got.(response.File)
	require.True(t, ok, "disabled link should render the HTML page")
	assert.Equal(t, htmlContentType, file.ContentType)

	body := string(file.Content)
	assert.Contains(t, body, "This link has been disabled")
	assert.Contains(t, body, "abuse@sho.rt")
	assert.NotContains(t, body, "malware", "coarse page must not leak categories")
	assert.NotContains(t, body, "abc1234", "no code echo needed")
}

func TestResolveHandler_Preview(t *testing.T) {
	newHandler := func(t *testing.T, contact string) (*ResolveHandler, *MockResolveService) {
		t.Helper()

		ctrl := gomock.NewController(t)
		svc := NewMockResolveService(ctrl)

		return NewResolveHandler(svc, NewMockAPIKeyService(ctrl), "", "sho.rt", contact), svc
	}

	previewCtx := func(t *testing.T) *gofr.Context {
		t.Helper()

		return newTestCtx(t, http.MethodGet, "/abc1234+", "", nil, map[string]string{"code": "abc1234"})
	}

	t.Run("active link renders destination + report form", func(t *testing.T) {
		h, svc := newHandler(t, "abuse@sho.rt")

		svc.EXPECT().PreviewByCode(gomock.Any(), "abc1234", gomock.Any()).
			Return(&services.Preview{Code: "abc1234", DestinationURL: "https://x.co/page?a=1"}, nil)

		got, err := h.Preview(previewCtx(t))

		require.NoError(t, err)
		file, ok := got.(response.File)
		require.True(t, ok)
		assert.Equal(t, htmlContentType, file.ContentType)

		body := string(file.Content)
		assert.Contains(t, body, "https://x.co/page?a=1", "destination shown as text")
		assert.Contains(t, body, "Report this link")
		assert.Contains(t, body, "/api/v1/report")
		assert.Contains(t, body, "abuse@sho.rt")
		assert.Contains(t, body, `"abc1234"`, "code embedded for the report POST")
	})

	t.Run("without ABUSE_CONTACT the contact line is absent", func(t *testing.T) {
		h, svc := newHandler(t, "")

		svc.EXPECT().PreviewByCode(gomock.Any(), "abc1234", gomock.Any()).
			Return(&services.Preview{Code: "abc1234", DestinationURL: "https://x.co"}, nil)

		got, err := h.Preview(previewCtx(t))

		require.NoError(t, err)
		assert.NotContains(t, string(got.(response.File).Content), "mailto:")
	})

	t.Run("disabled link renders the 410 page", func(t *testing.T) {
		h, svc := newHandler(t, "")

		svc.EXPECT().PreviewByCode(gomock.Any(), "abc1234", gomock.Any()).
			Return(&services.Preview{Code: "abc1234", DestinationURL: "https://x.co", Disabled: true}, nil)

		got, err := h.Preview(previewCtx(t))

		require.Error(t, err)

		var disabled *services.DisabledLinkError
		require.ErrorAs(t, err, &disabled)

		body := string(got.(response.File).Content)
		assert.Contains(t, body, "This link has been disabled")
		assert.NotContains(t, body, "https://x.co", "a disabled destination is never shown")
	})

	t.Run("unknown code renders an HTML 404", func(t *testing.T) {
		h, svc := newHandler(t, "")

		svc.EXPECT().PreviewByCode(gomock.Any(), "abc1234", gomock.Any()).
			Return(nil, apierrors.NotFound("short link not found"))

		got, err := h.Preview(previewCtx(t))

		require.Error(t, err)

		var apiErr apierrors.Error
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode())

		assert.Contains(t, string(got.(response.File).Content), "No such link")
	})

	t.Run("throttled preview passes the 429 through as JSON", func(t *testing.T) {
		h, svc := newHandler(t, "")

		svc.EXPECT().PreviewByCode(gomock.Any(), "abc1234", gomock.Any()).
			Return(nil, apierrors.TooManyRequests("too many requests"))

		got, err := h.Preview(previewCtx(t))

		require.Error(t, err)
		assert.Nil(t, got)
	})
}

func TestLinkHandler_CreateRateLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := NewMockLinkService(ctrl)
	keys := NewMockAPIKeyService(ctrl)
	h := NewLinkHandler(svc, keys, ratelimit.NewSlidingWindow(2, time.Minute))

	svc.EXPECT().Shorten(gomock.Any(), int64(3), int64(7), gomock.Any()).Return(nil, nil).Times(2)

	body := `{"url":"https://x.co"}`

	for range 2 {
		_, err := h.Shorten(newTestCtx(t, http.MethodPost, "/api/v1/links", body, orgOwnerClaims(), nil))
		require.NoError(t, err)
	}

	// Third create within the window: 429 before the service is touched.
	_, err := h.Shorten(newTestCtx(t, http.MethodPost, "/api/v1/links", body, orgOwnerClaims(), nil))

	require.Error(t, err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode())

	// Destination edits share the same budget: also 429 now.
	_, err = h.UpdateDestination(newTestCtx(t, http.MethodPatch, "/api/v1/links/9", body,
		orgOwnerClaims(), map[string]string{"id": "9"}))

	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode())
}
