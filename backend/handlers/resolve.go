package handlers

import (
	"errors"
	"net/http"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/visitor"
)

// htmlContentType is the content type of the inline safety pages.
const htmlContentType = "text/html; charset=utf-8"

// ResolveHandler serves the public resolution endpoints: GET /{code}
// (browser redirect) and GET /api/v1/resolve (JSON, programmatic). Both run
// the same pipeline and record a click; neither requires auth.
type ResolveHandler struct {
	resolver     ResolveService
	keys         APIKeyService
	websiteURL   string
	shortDomain  string
	abuseContact string
}

// NewResolveHandler wires a ResolveHandler. websiteURL is the optional
// bare-domain bounce target (WEBSITE_URL config); empty keeps GET / a 404.
// shortDomain classifies the request Host: the bounce belongs to the
// deployment's own domain only. abuseContact (ABUSE_CONTACT, may be empty)
// appears on the disabled-link and preview pages.
func NewResolveHandler(resolver ResolveService, keys APIKeyService,
	websiteURL, shortDomain, abuseContact string) *ResolveHandler {
	return &ResolveHandler{
		resolver: resolver, keys: keys,
		websiteURL: websiteURL, shortDomain: shortDomain, abuseContact: abuseContact,
	}
}

// Root handles GET / (bare domain, no code): 302 to WEBSITE_URL when
// configured, 404 otherwise. No click is recorded — the bare domain is not a
// short link, just a courtesy bounce to the website. The bounce is
// SHORT_DOMAIN-only: on a custom domain (or any other Host) the root is a
// 404 — an org's branded link domain must never redirect to this
// deployment's marketing site.
func (h *ResolveHandler) Root(ctx *gofr.Context) (any, error) {
	if h.websiteURL == "" || !services.IsDeploymentHost(visitor.FromContext(ctx).Host, h.shortDomain) {
		return nil, apierrors.NotFound("not found")
	}

	return response.Redirect{URL: h.websiteURL}, nil
}

// Redirect handles GET /{code}: 302 to the resolved destination. The
// Cache-Control: no-store header (a cached redirect kills click tracking) is
// set by the CacheControl middleware.
//
// A link disabled for abuse answers a 410 HTML warning page instead of a
// redirect — a browser visitor gets an explanation, not a JSON envelope.
// (gofr renders a File response with the error's status code, so the pair
// below is "HTML body, status 410".)
func (h *ResolveHandler) Redirect(ctx *gofr.Context) (any, error) {
	res, err := h.resolver.Resolve(ctx, ctx.PathParam("code"), "", visitor.FromContext(ctx))

	var disabled *services.DisabledLinkError
	if errors.As(err, &disabled) {
		return h.disabledPage(), err
	}

	if err != nil {
		return nil, err
	}

	return response.Redirect{URL: res.URL}, nil
}

// Preview handles GET /{code}+ — the link preview page (FEATURES.md
// "abuse protection"): the destination shown as text plus a report form,
// with NO click recorded. Works on custom domains with the same org
// scoping as resolution. Browser-facing, so every outcome is HTML: unknown
// codes render a 404 page, disabled links the 410 warning page.
func (h *ResolveHandler) Preview(ctx *gofr.Context) (any, error) {
	preview, err := h.resolver.PreviewByCode(ctx, ctx.PathParam("code"), visitor.FromContext(ctx))

	var apiErr apierrors.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode() == http.StatusNotFound {
		return response.File{
			Content:     renderPage(notFoundTmpl, pageData{Title: "No such link"}),
			ContentType: htmlContentType,
		}, err
	}

	if err != nil {
		return nil, err
	}

	if preview.Disabled {
		return h.disabledPage(), &services.DisabledLinkError{Code: preview.Code, AbuseContact: h.abuseContact}
	}

	return response.File{
		Content: renderPage(previewTmpl, pageData{
			Title:          "Link preview",
			Code:           preview.Code,
			DestinationURL: preview.DestinationURL,
			AbuseContact:   h.abuseContact,
		}),
		ContentType: htmlContentType,
	}, nil
}

// disabledPage renders the coarse 410 warning page.
func (h *ResolveHandler) disabledPage() response.File {
	return response.File{
		Content: renderPage(disabledTmpl, pageData{
			Title:        "This link has been disabled",
			AbuseContact: h.abuseContact,
		}),
		ContentType: htmlContentType,
	}
}

// Resolve handles GET /api/v1/resolve?code=&tag= — JSON resolution. tag is an
// optional campaign tag recorded on the click (FEATURES.md §5.3).
//
// The endpoint is public, but an explicitly supplied X-API-Key must be valid:
// a wrong or disabled key fails loudly with 401 rather than silently
// resolving as anonymous (documented decision, ARCHITECTURE.md §4). A valid
// key changes nothing about the resolution itself — it only stamps the key's
// last_used_at.
func (h *ResolveHandler) Resolve(ctx *gofr.Context) (any, error) {
	if rawKey, ok := auth.APIKeyFromContext(ctx); ok {
		if _, err := h.keys.Authenticate(ctx, rawKey); err != nil {
			return nil, err
		}
	}

	code := ctx.Param("code")
	if code == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"code"}}
	}

	return h.resolver.Resolve(ctx, code, ctx.Param("tag"), visitor.FromContext(ctx))
}
