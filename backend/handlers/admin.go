package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// AdminHandler serves the instance-admin API under /api/internal/*
// (ARCHITECTURE.md "Instance admin API"): the operator surface of a
// deployment. There is no org scoping here — this is the sanctioned INV-6
// exception; authentication is the ADMIN_API_TOKEN gate middleware
// (auth.AdminTokenGate), which 404s everything when the token is unset or
// wrong, so these handlers only ever run for a token-bearing operator.
type AdminHandler struct {
	admin AdminService
}

// NewAdminHandler wires an AdminHandler.
func NewAdminHandler(admin AdminService) *AdminHandler {
	return &AdminHandler{admin: admin}
}

// Users handles GET /api/internal/users?query=&page= — 25/page, each user
// with all its org memberships.
func (h *AdminHandler) Users(ctx *gofr.Context) (any, error) {
	page, err := pageParam(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — a typed nil pointer would make gofr respond 206.
	users, err := h.admin.Users(ctx, ctx.Param("query"), page)
	if err != nil {
		return nil, err
	}

	return users, nil
}

// Orgs handles GET /api/internal/orgs?query=&page= — 25/page with aggregate
// counts.
func (h *AdminHandler) Orgs(ctx *gofr.Context) (any, error) {
	page, err := pageParam(ctx)
	if err != nil {
		return nil, err
	}

	orgs, err := h.admin.Orgs(ctx, ctx.Param("query"), page)
	if err != nil {
		return nil, err
	}

	return orgs, nil
}

// Reports handles GET /api/internal/reports?page= — abuse reports newest
// first, 25/page.
func (h *AdminHandler) Reports(ctx *gofr.Context) (any, error) {
	page, err := pageParam(ctx)
	if err != nil {
		return nil, err
	}

	reports, err := h.admin.Reports(ctx, page)
	if err != nil {
		return nil, err
	}

	return reports, nil
}

// Link handles GET /api/internal/links/{id} — the operator view of one link
// in any org.
func (h *AdminHandler) Link(ctx *gofr.Context) (any, error) {
	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	link, err := h.admin.Link(ctx, id)
	if err != nil {
		return nil, err
	}

	return link, nil
}

// disableLinkRequest is the optional POST /api/internal/links/{id}/disable
// body.
type disableLinkRequest struct {
	Reason string `json:"reason"`
}

// DisableLink handles POST /api/internal/links/{id}/disable {reason?} —
// flips the link to DISABLED_ABUSE (410 page, no clicks recorded), logging
// the action with its reason.
func (h *AdminHandler) DisableLink(ctx *gofr.Context) (any, error) {
	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	var req disableLinkRequest

	if err := ctx.Bind(&req); err != nil {
		// The reason is optional, so an absent body is fine; anything else
		// unparseable is 400 (same empty-body detection as SetDeeplink).
		var syntaxErr *json.SyntaxError

		emptyBody := errors.Is(err, io.EOF) ||
			(errors.As(err, &syntaxErr) && syntaxErr.Offset == 0)
		if !emptyBody {
			return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
		}
	}

	link, err := h.admin.DisableLink(ctx, id, req.Reason)
	if err != nil {
		return nil, err
	}

	return link, nil
}

// EnableLink handles POST /api/internal/links/{id}/enable — flips the link
// back to ACTIVE.
func (h *AdminHandler) EnableLink(ctx *gofr.Context) (any, error) {
	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	link, err := h.admin.EnableLink(ctx, id)
	if err != nil {
		return nil, err
	}

	return link, nil
}

// DailyStats handles GET /api/internal/stats/daily?days=30 — instance-wide
// per-day signups/links/clicks (default 30 days, capped at 90 in the
// service).
func (h *AdminHandler) DailyStats(ctx *gofr.Context) (any, error) {
	days := 0

	if raw := ctx.Param("days"); raw != "" {
		var err error

		days, err = strconv.Atoi(raw)
		if err != nil || days < 1 {
			return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"days"}}
		}
	}

	stats, err := h.admin.DailyStats(ctx, days)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// pageParam parses ?page= (default 1; anything non-positive or non-numeric
// is 400).
func pageParam(ctx *gofr.Context) (int, error) {
	raw := ctx.Param("page")
	if raw == "" {
		return 1, nil
	}

	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 0, gofrHTTP.ErrorInvalidParam{Params: []string{"page"}}
	}

	return page, nil
}
