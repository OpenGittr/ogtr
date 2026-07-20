package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/services"
)

// LinkHandler serves /api/v1/links*. The org is the org_id claim of the
// access token — or, on Shorten with an X-API-Key header, the org the key
// belongs to.
type LinkHandler struct {
	links LinkService
	keys  APIKeyService
	// createLimiter caps link creation + destination edits per authenticated
	// principal (LINK_CREATE_RATE per minute; nil = unlimited). In-memory,
	// per-instance — see the ratelimit package doc for the limitation.
	createLimiter *ratelimit.SlidingWindow
}

// NewLinkHandler wires a LinkHandler.
func NewLinkHandler(links LinkService, keys APIKeyService, createLimiter *ratelimit.SlidingWindow) *LinkHandler {
	return &LinkHandler{links: links, keys: keys, createLimiter: createLimiter}
}

// allowWrite applies the creation rate limit for one authenticated
// principal ("user:<id>" or "key:<id>").
func (h *LinkHandler) allowWrite(principal string) error {
	if h.createLimiter != nil && !h.createLimiter.Allow(principal) {
		return apierrors.TooManyRequests("you're creating or editing links too quickly — try again in a minute")
	}

	return nil
}

// Shorten handles POST /api/v1/links. Shortening a destination the org
// already has (or a URL that is already a short link here) returns the
// existing link. With an X-API-Key header (forwarded by the auth middleware)
// the key authenticates instead of a JWT: it resolves to its own org and the
// link records api_key_id with no user.
func (h *LinkHandler) Shorten(ctx *gofr.Context) (any, error) {
	var in services.ShortenInput
	if err := ctx.Bind(&in); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if in.URL == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"url"}}
	}

	if rawKey, ok := auth.APIKeyFromContext(ctx); ok {
		key, err := h.keys.Authenticate(ctx, rawKey)
		if err != nil {
			return nil, err
		}

		if err := h.allowWrite("key:" + strconv.FormatInt(key.ID, 10)); err != nil {
			return nil, err
		}

		// Untyped nil on error — a typed nil pointer in the any return
		// makes gofr respond 206.
		link, err := h.links.ShortenViaAPIKey(ctx, key.OrgID, key.ID, in)
		if err != nil {
			return nil, err
		}

		return link, nil
	}

	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.allowWrite("user:" + strconv.FormatInt(claims.UserID, 10)); err != nil {
		return nil, err
	}

	return h.links.Shorten(ctx, claims.OrgID, claims.UserID, in)
}

// List handles GET /api/v1/links?page= (10 per page).
func (h *LinkHandler) List(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	page := 1

	if raw := ctx.Param("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"page"}}
		}
	}

	return h.links.List(ctx, claims.OrgID, claims.UserID, page)
}

// Get handles GET /api/v1/links/{id}.
func (h *LinkHandler) Get(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	return h.links.Get(ctx, claims.OrgID, claims.UserID, id)
}

type setAliasRequest struct {
	Alias string `json:"alias"`
}

// SetAlias handles PUT /api/v1/links/{id}/alias.
func (h *LinkHandler) SetAlias(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	var req setAliasRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Alias == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"alias"}}
	}

	return h.links.SetAlias(ctx, claims.OrgID, claims.UserID, id, req.Alias)
}

// UpdateDestination handles PATCH /api/v1/links/{id} — destination editing
// (FEATURES.md §1.5). Permission (link creator or org OWNER) and validation
// live in the service; every applied edit writes a link_edits audit row.
func (h *LinkHandler) UpdateDestination(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	var in services.EditInput
	if err := ctx.Bind(&in); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if in.URL == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"url"}}
	}

	// Destination edits share the creation rate limit: repointing links is
	// the same abuse surface as creating them.
	if err := h.allowWrite("user:" + strconv.FormatInt(claims.UserID, 10)); err != nil {
		return nil, err
	}

	// Untyped nil on error — a typed nil pointer in the any return makes
	// gofr respond 206.
	link, err := h.links.UpdateDestination(ctx, claims.OrgID, claims.UserID, id, in)
	if err != nil {
		return nil, err
	}

	return link, nil
}

// SetDeeplink handles PUT /api/v1/links/{id}/deeplink — the owner-set mobile
// deep-link config (FEATURES.md §3.2). An empty or null body clears it.
func (h *LinkHandler) SetDeeplink(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	var cfg models.DeeplinkConfig

	if err := ctx.Bind(&cfg); err != nil {
		// An absent body means "clear the config" (like a JSON null);
		// anything else unparseable is 400. gofr binds via json.Unmarshal, so
		// an empty body surfaces as a SyntaxError at offset 0 (io.EOF covered
		// for robustness).
		var syntaxErr *json.SyntaxError

		emptyBody := errors.Is(err, io.EOF) ||
			(errors.As(err, &syntaxErr) && syntaxErr.Offset == 0)
		if !emptyBody {
			return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
		}
	}

	return h.links.SetDeeplink(ctx, claims.OrgID, claims.UserID, id, &cfg)
}

// QR handles GET /api/v1/links/{id}/qr — the short URL as a PNG QR code.
// The immutable Cache-Control header is set by the CacheControl middleware.
func (h *LinkHandler) QR(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	png, err := h.links.QRCodePNG(ctx, claims.OrgID, claims.UserID, id)
	if err != nil {
		return nil, err
	}

	return response.File{Content: png, ContentType: "image/png"}, nil
}
