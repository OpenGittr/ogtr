package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// DomainHandler serves /api/v1/org/domains — per-org custom short domains
// (FEATURES.md §1.6). Org-scoped via the access token; mutations are
// OWNER-only (checked in the service against the DB role), members may list.
type DomainHandler struct {
	domains DomainService
}

// NewDomainHandler wires a DomainHandler.
func NewDomainHandler(domains DomainService) *DomainHandler {
	return &DomainHandler{domains: domains}
}

type createDomainRequest struct {
	Hostname string `json:"hostname"`
}

// Create handles POST /api/v1/org/domains: registers a hostname as PENDING
// and returns it with the TXT record (name + value) to publish. A hostname
// registered by any org is 409; invalid hostnames and the deployment's own
// short domain (or a subdomain of it) are 422.
func (h *DomainHandler) Create(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	var req createDomainRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Hostname == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"hostname"}}
	}

	// Untyped nil on error — a typed nil pointer in the any return makes
	// gofr respond 206.
	domain, err := h.domains.Create(ctx, claims.OrgID, claims.UserID, req.Hostname)
	if err != nil {
		return nil, err
	}

	return domain, nil
}

// List handles GET /api/v1/org/domains — every member sees the org's domains
// with status and TXT instructions.
func (h *DomainHandler) List(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — returning the service's typed nil slice
	// alongside an error makes gofr respond 206.
	domains, err := h.domains.List(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return domains, nil
}

// Verify handles POST /api/v1/org/domains/{id}/verify: runs the DNS TXT
// check. 200 with the VERIFIED domain on success (idempotent for an
// already-verified domain); 409 with a human-readable reason while DNS does
// not prove ownership (record missing, wrong value, lookup failure).
func (h *DomainHandler) Verify(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Create.
	domain, err := h.domains.Verify(ctx, claims.OrgID, claims.UserID, id)
	if err != nil {
		return nil, err
	}

	return domain, nil
}

// SetPrimary handles PUT /api/v1/org/domains/{id}/primary: makes this the
// org's single primary domain (transactional swap; VERIFIED only).
func (h *DomainHandler) SetPrimary(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Create.
	domain, err := h.domains.SetPrimary(ctx, claims.OrgID, claims.UserID, id)
	if err != nil {
		return nil, err
	}

	return domain, nil
}

// Delete handles DELETE /api/v1/org/domains/{id}: removes the domain. Short
// URLs revert to the deployment's SHORT_DOMAIN display; the links themselves
// keep resolving there.
func (h *DomainHandler) Delete(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	if err := h.domains.Delete(ctx, claims.OrgID, claims.UserID, id); err != nil {
		return nil, err
	}

	return map[string]bool{"deleted": true}, nil
}
