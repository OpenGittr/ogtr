package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/services"
)

// BrandPrefixHandler serves /api/v1/org/prefixes — per-org brand prefixes
// (FEATURES.md §1.7). Org-scoped via the access token; mutations are
// OWNER-only (checked in the service against the DB role), members may list.
// Operator approval lives on the instance admin service.
type BrandPrefixHandler struct {
	prefixes BrandPrefixService
}

// NewBrandPrefixHandler wires a BrandPrefixHandler.
func NewBrandPrefixHandler(prefixes BrandPrefixService) *BrandPrefixHandler {
	return &BrandPrefixHandler{prefixes: prefixes}
}

// Create handles POST /api/v1/org/prefixes {prefix, registry, entity_id,
// header, entity_name}: files the request as PENDING (APPROVED under
// auto-approval). Invalid input and reserved prefixes are 422; a prefix held
// by any org, or equal to an existing short code, is 409.
func (h *BrandPrefixHandler) Create(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	var req services.BrandPrefixInput
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Prefix == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"prefix"}}
	}

	// Untyped nil on error — a typed nil pointer in the any return makes
	// gofr respond 206.
	p, err := h.prefixes.Create(ctx, claims.OrgID, claims.UserID, req)
	if err != nil {
		return nil, err
	}

	return p, nil
}

// List handles GET /api/v1/org/prefixes — every member sees the org's
// prefixes with status and the operator's decision reason.
func (h *BrandPrefixHandler) List(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	prefixes, err := h.prefixes.List(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return prefixes, nil
}

// SetPrimary handles PUT /api/v1/org/prefixes/{id}/primary: makes this the
// org's display prefix (APPROVED only).
func (h *BrandPrefixHandler) SetPrimary(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	p, err := h.prefixes.SetPrimary(ctx, claims.OrgID, claims.UserID, id)
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Delete handles DELETE /api/v1/org/prefixes/{id}: withdraws a request or
// removes a prefix; a REVOKED prefix is 409 (held until operator release).
func (h *BrandPrefixHandler) Delete(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	if err := h.prefixes.Delete(ctx, claims.OrgID, claims.UserID, id); err != nil {
		return nil, err
	}

	return map[string]bool{"deleted": true}, nil
}
