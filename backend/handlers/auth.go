package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
)

// AuthHandler serves the /api/v1/auth/* and /api/v1/me endpoints.
//
// The login routes are always registered; the handler answers 404 for a
// provider that is not in AUTH_PROVIDERS (same effect as an unregistered
// route, but unit-testable and with a clear message).
type AuthHandler struct {
	authSvc AuthService
	// providers preserves AUTH_PROVIDERS order for GET /auth/providers.
	providers []string
	enabled   map[string]bool
	// googleClientID / microsoftClientID are served to the SPA so it needs no
	// build-time copy of the client IDs (client IDs are public identifiers,
	// not secrets).
	googleClientID    string
	microsoftClientID string
}

// NewAuthHandler wires an AuthHandler. providers is the validated
// AUTH_PROVIDERS list (auth.ParseProviders).
func NewAuthHandler(authSvc AuthService, providers []string, googleClientID, microsoftClientID string) *AuthHandler {
	enabled := make(map[string]bool, len(providers))
	for _, p := range providers {
		enabled[p] = true
	}

	return &AuthHandler{
		authSvc: authSvc, providers: providers, enabled: enabled,
		googleClientID: googleClientID, microsoftClientID: microsoftClientID,
	}
}

var errProviderDisabled = apierrors.NotFound("this sign-in method is not enabled on this server")

// ProvidersResponse is the GET /api/v1/auth/providers payload: which sign-in
// methods this deployment offers, so the SPA renders the login page
// dynamically instead of hardcoding Google.
type ProvidersResponse struct {
	Providers []string `json:"providers"`
	// GoogleClientID is empty unless the google provider is enabled.
	GoogleClientID string `json:"google_client_id"`
	// MicrosoftClientID is empty unless the microsoft provider is enabled.
	MicrosoftClientID string `json:"microsoft_client_id"`
}

// Providers handles GET /api/v1/auth/providers (public).
func (h *AuthHandler) Providers(*gofr.Context) (any, error) {
	googleID, microsoftID := "", ""
	if h.enabled[auth.ProviderGoogle] {
		googleID = h.googleClientID
	}

	if h.enabled[auth.ProviderMicrosoft] {
		microsoftID = h.microsoftClientID
	}

	return ProvidersResponse{Providers: h.providers, GoogleClientID: googleID, MicrosoftClientID: microsoftID}, nil
}

type idTokenLoginRequest struct {
	IDToken string `json:"id_token"`
}

// GoogleLogin handles POST /api/v1/auth/google. 404 when the google provider
// is not enabled.
func (h *AuthHandler) GoogleLogin(ctx *gofr.Context) (any, error) {
	if !h.enabled[auth.ProviderGoogle] {
		return nil, errProviderDisabled
	}

	var req idTokenLoginRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.IDToken == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"id_token"}}
	}

	return h.authSvc.Login(ctx, auth.ProviderGoogle, req.IDToken)
}

// MicrosoftLogin handles POST /api/v1/auth/microsoft. 404 when the microsoft
// provider is not enabled. The SPA obtains the ID token itself via the PKCE
// authorization-code flow against login.microsoftonline.com and posts it
// here; from this point the flow is identical to Google.
func (h *AuthHandler) MicrosoftLogin(ctx *gofr.Context) (any, error) {
	if !h.enabled[auth.ProviderMicrosoft] {
		return nil, errProviderDisabled
	}

	var req idTokenLoginRequest // same {id_token} shape for every OIDC provider
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.IDToken == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"id_token"}}
	}

	return h.authSvc.Login(ctx, auth.ProviderMicrosoft, req.IDToken)
}

type devLoginRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// DevLogin handles POST /api/v1/auth/dev. 404 when the dev provider is not
// enabled (the endpoint then behaves as if it did not exist). The email/name
// pair is packed into a credential string and flows through the exact same
// service login path as Google — semantic validation (email format,
// non-empty name → 422) lives in auth.DevProvider.Verify.
func (h *AuthHandler) DevLogin(ctx *gofr.Context) (any, error) {
	if !h.enabled[auth.ProviderDev] {
		return nil, errProviderDisabled
	}

	var req devLoginRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	return h.authSvc.Login(ctx, auth.ProviderDev, auth.EncodeDevCredential(req.Email, req.Name))
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh handles POST /api/v1/auth/refresh.
func (h *AuthHandler) Refresh(ctx *gofr.Context) (any, error) {
	var req refreshRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.RefreshToken == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"refresh_token"}}
	}

	pair, err := h.authSvc.Refresh(ctx, req.RefreshToken)
	if err != nil {
		return nil, err
	}

	return pair, nil
}

// Me handles GET /api/v1/me.
func (h *AuthHandler) Me(ctx *gofr.Context) (any, error) {
	claims, err := auth.ClaimsFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return h.authSvc.Me(ctx, claims)
}

type switchOrgRequest struct {
	OrgID int64 `json:"org_id"`
}

// SwitchOrg handles POST /api/v1/auth/switch-org.
func (h *AuthHandler) SwitchOrg(ctx *gofr.Context) (any, error) {
	claims, err := auth.ClaimsFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var req switchOrgRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.OrgID <= 0 {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"org_id"}}
	}

	pair, err := h.authSvc.SwitchOrg(ctx, claims.UserID, req.OrgID)
	if err != nil {
		return nil, err
	}

	return pair, nil
}
