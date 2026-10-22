package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newBrandPrefixHandler(t *testing.T) (*BrandPrefixHandler, *MockBrandPrefixService) {
	t.Helper()

	svc := NewMockBrandPrefixService(gomock.NewController(t))

	return NewBrandPrefixHandler(svc), svc
}

func TestBrandPrefixHandler_Create(t *testing.T) {
	created := &models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusPending}
	body := `{"prefix":"ACMEIN","entity_id":"1101","header":"ACMEIN","entity_name":"Acme"}`

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockBrandPrefixService)
		wantErr bool
	}{
		{
			desc: "request filed",
			body: body,
			setup: func(svc *MockBrandPrefixService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), services.BrandPrefixInput{
					Prefix: "ACMEIN", EntityID: "1101", Header: "ACMEIN", EntityName: "Acme",
				}).Return(created, nil)
			},
		},
		{desc: "missing prefix", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "org-less token rejected", body: body, orgless: true, wantErr: true},
		{
			desc: "service conflict surfaces",
			body: body,
			setup: func(svc *MockBrandPrefixService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), gomock.Any()).
					Return(nil, apierrors.Conflict("this prefix is already taken"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newBrandPrefixHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/prefixes", tc.body, claims, nil)

			got, err := h.Create(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "untyped nil on error")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, created, got)
		})
	}
}

// newMultipartCtx builds an owner's multipart/form-data request carrying the
// given fields and, when non-nil, a "screenshot" file.
func newMultipartCtx(t *testing.T, fields map[string]string, screenshot []byte) *gofr.Context {
	t.Helper()

	var body bytes.Buffer

	w := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}

	if screenshot != nil {
		fw, err := w.CreateFormFile("screenshot", "dlt.png")
		require.NoError(t, err)

		_, err = fw.Write(screenshot)
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/org/prefixes", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = req.WithContext(auth.ContextWithClaims(context.Background(), orgOwnerClaims()))

	mockContainer, _ := container.NewMockContainer(t)

	return &gofr.Context{Context: req.Context(), Request: gofrHTTP.NewRequest(req), Container: mockContainer}
}

func TestBrandPrefixHandler_CreateMultipart(t *testing.T) {
	created := &models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusPending}
	fields := map[string]string{"prefix": "ACMEIN", "entity_id": "1101", "header": "ACMEIN", "entity_name": "Acme"}

	t.Run("fields and screenshot reach the service", func(t *testing.T) {
		h, svc := newBrandPrefixHandler(t)
		svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), services.BrandPrefixInput{
			Prefix: "ACMEIN", EntityID: "1101", Header: "ACMEIN", EntityName: "Acme", Screenshot: []byte("png-bytes"),
		}).Return(created, nil)

		got, err := h.Create(newMultipartCtx(t, fields, []byte("png-bytes")))

		require.NoError(t, err)
		assert.Equal(t, created, got)
	})

	t.Run("oversized screenshot is cut one byte past the cap", func(t *testing.T) {
		h, svc := newBrandPrefixHandler(t)
		svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), gomock.Any()).DoAndReturn(
			func(_ *gofr.Context, _, _ int64, in services.BrandPrefixInput) (*models.BrandPrefix, error) {
				assert.Len(t, in.Screenshot, services.MaxScreenshotBytes+1)

				return nil, apierrors.Unprocessable("screenshot must be at most 2 MB")
			})

		_, err := h.Create(newMultipartCtx(t, fields, make([]byte, services.MaxScreenshotBytes+100)))

		require.Error(t, err)
	})

	t.Run("no screenshot leaves it empty for the service to reject", func(t *testing.T) {
		h, svc := newBrandPrefixHandler(t)
		svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), services.BrandPrefixInput{
			Prefix: "ACMEIN", EntityID: "1101", Header: "ACMEIN", EntityName: "Acme",
		}).Return(nil, apierrors.Unprocessable("attach a screenshot of this header on your DLT portal"))

		_, err := h.Create(newMultipartCtx(t, fields, nil))

		require.Error(t, err)
	})
}

func TestAdminHandler_PrefixScreenshot(t *testing.T) {
	vars := map[string]string{"id": "5"}

	t.Run("served as the image itself", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().PrefixScreenshot(gomock.Any(), int64(5)).
			Return(&models.BrandPrefixScreenshot{ContentType: "image/png", Image: []byte("png")}, nil)

		got, err := h.PrefixScreenshot(newTestCtx(t, http.MethodGet, "/api/internal/prefixes/5/screenshot", "", nil, vars))

		require.NoError(t, err)
		assert.Equal(t, response.File{Content: []byte("png"), ContentType: "image/png"}, got)
	})

	t.Run("missing screenshot surfaces the 404", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().PrefixScreenshot(gomock.Any(), int64(5)).
			Return(nil, apierrors.NotFound("no screenshot for this prefix"))

		got, err := h.PrefixScreenshot(newTestCtx(t, http.MethodGet, "/api/internal/prefixes/5/screenshot", "", nil, vars))

		require.Error(t, err)
		assert.Nil(t, got)
	})
}

func TestBrandPrefixHandler_ListPrimaryDelete(t *testing.T) {
	h, svc := newBrandPrefixHandler(t)
	approved := &models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusApproved}

	svc.EXPECT().List(gomock.Any(), int64(3)).Return([]models.BrandPrefix{*approved}, nil)
	svc.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(7), int64(5)).Return(approved, nil)
	svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(5)).Return(nil)

	list, err := h.List(newTestCtx(t, http.MethodGet, "/api/v1/org/prefixes", "", orgOwnerClaims(), nil))
	require.NoError(t, err)
	assert.Len(t, list, 1)

	vars := map[string]string{"id": "5"}

	primary, err := h.SetPrimary(newTestCtx(t, http.MethodPut, "/api/v1/org/prefixes/5/primary", "", orgOwnerClaims(), vars))
	require.NoError(t, err)
	assert.Equal(t, approved, primary)

	deleted, err := h.Delete(newTestCtx(t, http.MethodDelete, "/api/v1/org/prefixes/5", "", orgOwnerClaims(), vars))
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"deleted": true}, deleted)

	_, err = h.SetPrimary(newTestCtx(t, http.MethodPut, "/api/v1/org/prefixes/x/primary", "", orgOwnerClaims(),
		map[string]string{"id": "x"}))
	require.Error(t, err, "non-numeric id is 400")
}

func TestAdminHandler_PrefixDecisions(t *testing.T) {
	approved := &models.AdminBrandPrefix{BrandPrefix: models.BrandPrefix{ID: 5, Status: models.BrandPrefixStatusApproved}}
	vars := map[string]string{"id": "5"}

	t.Run("queue passes status and page", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().Prefixes(gomock.Any(), "PENDING", 2).Return(&services.AdminPrefixesPage{}, nil)

		_, err := h.Prefixes(newTestCtx(t, http.MethodGet, "/api/internal/prefixes?status=PENDING&page=2", "", nil, nil))

		require.NoError(t, err)
	})

	t.Run("approve", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().ApprovePrefix(gomock.Any(), int64(5)).Return(approved, nil)

		got, err := h.ApprovePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/approve", "", nil, vars))

		require.NoError(t, err)
		assert.Equal(t, approved, got)
	})

	t.Run("reject passes the reason", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().RejectPrefix(gomock.Any(), int64(5), "not your header").Return(approved, nil)

		_, err := h.RejectPrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/reject",
			`{"reason":"not your header"}`, nil, vars))

		require.NoError(t, err)
	})

	t.Run("revoke with garbage body is 400", func(t *testing.T) {
		h, _ := newAdminHandler(t)

		_, err := h.RevokePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/revoke", `{`, nil, vars))

		require.Error(t, err)
	})

	t.Run("release", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().ReleasePrefix(gomock.Any(), int64(5)).Return(nil)

		got, err := h.ReleasePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/release", "", nil, vars))

		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"released": true}, got)
	})
}

func TestResolveHandler_RedirectPassesPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := NewMockResolveService(ctrl)
	h := NewResolveHandler(svc, NewMockAPIKeyService(ctrl), "", "sho.rt", "")

	svc.EXPECT().Resolve(gomock.Any(), "ACMEIN", "abc1234", "", gomock.Any()).
		Return(&services.Resolution{Code: "abc1234", URL: "https://example.com"}, nil)

	ctx := newTestCtx(t, http.MethodGet, "/ACMEIN/abc1234", "", nil,
		map[string]string{"prefix": "ACMEIN", "code": "abc1234"})

	_, err := h.Redirect(ctx)

	require.NoError(t, err)
}
