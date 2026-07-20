package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// APIKeyHandler serves /api/v1/api-keys — developer API key management
// (FEATURES.md §8). Org-scoped via the access token; any org member can
// manage keys (documented decision, ARCHITECTURE.md §4).
type APIKeyHandler struct {
	keys APIKeyService
}

// NewAPIKeyHandler wires an APIKeyHandler.
func NewAPIKeyHandler(keys APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{keys: keys}
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

// Create handles POST /api/v1/api-keys. The response carries the plaintext
// key exactly once — it is never retrievable again.
func (h *APIKeyHandler) Create(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	var req createAPIKeyRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	if req.Name == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"name"}}
	}

	// Untyped nil on error — a typed nil pointer in the any return makes
	// gofr respond 206.
	key, err := h.keys.Create(ctx, claims.OrgID, req.Name)
	if err != nil {
		return nil, err
	}

	return key, nil
}

// List handles GET /api/v1/api-keys — no key material beyond the hint.
func (h *APIKeyHandler) List(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Create.
	keys, err := h.keys.List(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return keys, nil
}

// Disable handles DELETE /api/v1/api-keys/{id}: sets the key DISABLED (keys
// are never hard-deleted — link attribution history survives). Cross-org
// ids are 404.
func (h *APIKeyHandler) Disable(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Create.
	key, err := h.keys.Disable(ctx, claims.OrgID, id)
	if err != nil {
		return nil, err
	}

	return key, nil
}
