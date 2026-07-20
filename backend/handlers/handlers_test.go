package handlers

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/auth"
)

// newTestCtx builds a gofr context for a JSON request, optionally carrying
// session claims (as the auth middleware would) and mux path variables.
func newTestCtx(t *testing.T, method, path, body string, claims *auth.SessionClaims, vars map[string]string) *gofr.Context {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	reqCtx := context.Context(req.Context())
	if claims != nil {
		reqCtx = auth.ContextWithClaims(reqCtx, claims)
	}

	req = req.WithContext(reqCtx)

	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}

	mockContainer, _ := container.NewMockContainer(t)

	return &gofr.Context{
		Context:   req.Context(),
		Request:   gofrHTTP.NewRequest(req),
		Container: mockContainer,
	}
}

func orgOwnerClaims() *auth.SessionClaims {
	return &auth.SessionClaims{UserID: 7, OrgID: 3, Role: "OWNER", TokenType: auth.TokenTypeAccess}
}

func orglessClaims() *auth.SessionClaims {
	return &auth.SessionClaims{UserID: 7, TokenType: auth.TokenTypeAccess}
}
