package main

import "github.com/opengittr/ogtr/backend/app"

// main runs the stock assembly (backend/app) with no options: unlimited
// policy, core migrations and routes only. A deployment that composes the
// core with additions has its own main calling app.Run with options
// (ARCHITECTURE.md §8 "Deployment composition").
func main() {
	app.Run()
}
