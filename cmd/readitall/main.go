package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lukaszraczylo/mcp-readitall/internal/server"
)

func main() {
	logger := log.New(os.Stderr, "[readitall] ", log.LstdFlags|log.Lmsgprefix)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.New(ctx, logger)
	if err != nil {
		logger.Fatalf("init server: %v", err)
	}
	defer func() {
		if cerr := srv.Close(); cerr != nil {
			logger.Printf("shutdown: %v", cerr)
		}
	}()

	addr := os.Getenv("READITALL_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	logger.Printf("starting MCP server (http) on %s (/mcp, /sse)", addr)
	if err := srv.RunHTTP(ctx, addr); err != nil {
		logger.Fatalf("server exited: %v", err)
	}
}
