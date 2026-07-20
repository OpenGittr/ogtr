package handlers

import (
	"strconv"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

// OrgHandler serves /api/v1/orgs and /api/v1/org/*. The active org is always
// the org_id claim of the access token — never a path or body field.
type OrgHandler struct {
	orgSvc  OrgService
	authSvc AuthService
}

// NewOrgHandler wires an OrgHandler.
func NewOrgHandler(orgSvc OrgService, authSvc AuthService) *OrgHandler {
	return &OrgHandler{orgSvc: orgSvc, authSvc: authSvc}
}

type createOrgRequest struct {
	Name           string `json:"name"`
	AutoJoinDomain string `json:"auto_join_domain"`
}

type createOrgResponse struct {
	Org *models.Org `json:"org"`
	models.TokenPair
}

// Create handles POST /api/v1/orgs. Works for org-less tokens (first org).
// The response includes a token pair scoped to the new org so the SPA does
// not need a follow-up switch-org call.
func (h *OrgHandler) Create(ctx *gofr.Context) (any, error) {
	claims, err := auth.ClaimsFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var req createOrgRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Name == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"name"}}
	}

	org, err := h.orgSvc.Create(ctx, claims.UserID, req.Name, req.AutoJoinDomain)
	if err != nil {
		return nil, err
	}

	pair, err := h.authSvc.SwitchOrg(ctx, claims.UserID, org.ID)
	if err != nil {
		return nil, err
	}

	return createOrgResponse{Org: org, TokenPair: pair}, nil
}

// Get handles GET /api/v1/org.
func (h *OrgHandler) Get(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	return h.orgSvc.Get(ctx, claims.OrgID)
}

// Update handles PATCH /api/v1/org (OWNER only).
func (h *OrgHandler) Update(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	var patch services.OrgUpdate
	if err := ctx.Bind(&patch); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	return h.orgSvc.Update(ctx, claims.OrgID, claims.UserID, patch)
}

// Members handles GET /api/v1/org/members.
func (h *OrgHandler) Members(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — returning the service's typed nil slice alongside
	// an error makes gofr respond 206.
	members, err := h.orgSvc.Members(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return members, nil
}

// RemoveMember handles DELETE /api/v1/org/members/{userId} (OWNER only).
func (h *OrgHandler) RemoveMember(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	targetID, err := pathID(ctx, "userId")
	if err != nil {
		return nil, err
	}

	if err := h.orgSvc.RemoveMember(ctx, claims.OrgID, claims.UserID, targetID); err != nil {
		return nil, err
	}

	return map[string]bool{"removed": true}, nil
}

type createInviteRequest struct {
	Email string `json:"email"`
}

// CreateInvite handles POST /api/v1/org/invites (OWNER only).
func (h *OrgHandler) CreateInvite(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	var req createInviteRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Email == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"email"}}
	}

	return h.orgSvc.CreateInvite(ctx, claims.OrgID, claims.UserID, req.Email)
}

// ListInvites handles GET /api/v1/org/invites.
func (h *OrgHandler) ListInvites(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Members.
	invites, err := h.orgSvc.ListInvites(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return invites, nil
}

// RevokeInvite handles DELETE /api/v1/org/invites/{id} (OWNER only).
func (h *OrgHandler) RevokeInvite(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	inviteID, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	if err := h.orgSvc.RevokeInvite(ctx, claims.OrgID, claims.UserID, inviteID); err != nil {
		return nil, err
	}

	return map[string]bool{"revoked": true}, nil
}

// orgClaims returns the request's claims, rejecting org-less tokens.
func orgClaims(ctx *gofr.Context) (*auth.SessionClaims, error) {
	claims, err := auth.ClaimsFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := auth.RequireOrg(claims); err != nil {
		return nil, err
	}

	return claims, nil
}

func pathID(ctx *gofr.Context, param string) (int64, error) {
	id, err := strconv.ParseInt(ctx.PathParam(param), 10, 64)
	if err != nil || id <= 0 {
		return 0, gofrHTTP.ErrorInvalidParam{Params: []string{param}}
	}

	return id, nil
}
