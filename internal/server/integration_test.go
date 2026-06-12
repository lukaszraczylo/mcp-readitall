//go:build integration

// Package integration_test exercises the MCP server end-to-end through a
// real chromedp-managed Chromium. It is gated by the `integration` build
// tag so the default `go test ./...` run stays hermetic.
//
// Run with:
//
//	go test -tags=integration -race -v ./internal/server/...
//
// Make sure a Chromium or Chrome binary is on the system (chromedp will
// find it via the default Chrome discovery rules; override with
// chromedp.ExecPath if needed).
package server
