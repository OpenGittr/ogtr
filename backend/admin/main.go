// The ogtr instance-admin service — the operator surface of a deployment
// (ARCHITECTURE.md "Instance admin service"), split out of the public server
// so the cross-org admin API never shares a listener with internet-facing
// traffic. Deployed as `ogtr-internal` (k8s/internal-deployment.yaml): a
// cluster-internal ClusterIP service that must NEVER be exposed on any
// public ingress. The directory is `backend/admin` (not `backend/internal`)
// only because `internal/` is Go's reserved import-restriction directory
// name; the component/deployment name stays "ogtr-internal".
//
// One concern only: the /api/internal/* routes, guarded by the
// ADMIN_API_TOKEN gate (gate.go). No JWT middleware, no resolution, no
// migrations — schema is owned by the public server (backend/server), which
// migrates at boot; this service only reads/writes the same database
// through the shared stores/services packages.
package main

import (
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/handlers"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/stores"
)

func main() {
	app := gofr.New()

	// ADMIN_API_TOKEN unset (the default) keeps every /api/internal/* route
	// answering 404 — the service is dark, like the dev provider.
	adminToken := app.Config.Get("ADMIN_API_TOKEN")
	if adminToken == "" {
		app.Logger().Warnf("ADMIN_API_TOKEN is not set — every /api/internal/* endpoint answers 404")
	}

	// adminSvc reuses linkStore's SetStatusByID — operator disable/enable
	// shares the re-scan's abuse-status machinery exactly (public server).
	adminSvc := services.NewAdminService(stores.NewAdminStore(), stores.NewLinkStore())
	adminH := handlers.NewAdminHandler(adminSvc)

	app.UseMiddleware(AdminTokenGate(adminToken))

	// Instance admin API (deliberately cross-org — the sanctioned INV-6
	// exception; FEATURES.md §11). Same /api/internal/* paths as when these
	// routes lived on the public server: the console-gateway contract is
	// unchanged.
	app.GET("/api/internal/users", adminH.Users)
	app.GET("/api/internal/orgs", adminH.Orgs)
	app.GET("/api/internal/reports", adminH.Reports)
	app.GET("/api/internal/links/{id}", adminH.Link)
	app.POST("/api/internal/links/{id}/disable", adminH.DisableLink)
	app.POST("/api/internal/links/{id}/enable", adminH.EnableLink)
	app.GET("/api/internal/stats/daily", adminH.DailyStats)

	app.Run()
}
