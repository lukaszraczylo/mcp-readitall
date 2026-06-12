// Package main is a tiny end-to-end smoke harness for the readitall reader.
// It is intentionally separate from cmd/readitall (the MCP server) and lives
// here so you can verify the browser + markdown pipeline without an MCP
// client in the loop.
//
// Usage:
//
//	go run ./cmd/demo <url> [css-selector]
//
// Example:
//
//	go run ./cmd/demo https://raczylo.com
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
	"github.com/lukaszraczylo/mcp-readitall/internal/reader"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: demo <url> [css-selector]")
		os.Exit(2)
	}
	url := os.Args[1]
	var selector string
	if len(os.Args) >= 3 {
		selector = os.Args[2]
	}

	logger := log.New(os.Stderr, "[demo] ", log.LstdFlags|log.Lmsgprefix)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sess, err := browser.NewSessionStore()
	if err != nil {
		logger.Fatalf("session store: %v", err)
	}
	defer sess.Close()

	pw, err := browser.New(ctx, logger)
	if err != nil {
		logger.Fatalf("browser: %v", err)
	}
	defer pw.Close()

	r := reader.New(pw, sess, logger)
	res, err := r.Read(ctx, reader.ReadOptions{
		URL:      url,
		Selector: selector,
		Timeout:  60 * time.Second,
	})
	if err != nil {
		logger.Fatalf("read: %v", err)
	}

	fmt.Printf("--- meta ---\n")
	fmt.Printf("requested: %s\n", res.URL)
	fmt.Printf("final:     %s\n", res.FinalURL)
	fmt.Printf("title:     %s\n", res.Title)
	fmt.Printf("used_session: %v\n", res.UsedSession)
	fmt.Printf("truncated:    %v\n", res.Truncated)
	fmt.Printf("bytes:        %d\n", res.Bytes)
	fmt.Printf("--- markdown ---\n")
	fmt.Println(res.Markdown)
}
