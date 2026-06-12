package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
	"github.com/lukaszraczylo/mcp-readitall/internal/reader"
	"github.com/lukaszraczylo/mcp-readitall/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wires the MCP server, browser lifecycle, session store, and reader
// together, and exposes the tool surface to MCP clients.
type Server struct {
	mcp   *mcp.Server
	br    *browser.Browser
	sess  *browser.SessionStore
	readr *reader.Reader
	log   *log.Logger

	closeOnce sync.Once
	closeErr  error
}

const (
	serverName    = "mcp-readitall"
	serverVersion = "0.1.0"
)

// New constructs the MCP server, initialises the browser manager and the
// session store, and registers every tool. It does not start serving: call
// RunHTTP for that.
func New(ctx context.Context, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.Default()
	}

	sess, err := browser.NewSessionStore()
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}

	br, err := browser.New(ctx, logger)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("browser: %w", err)
	}

	readr := reader.New(br, sess, logger)

	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion},
		nil,
	)

	s := &Server{
		mcp:   mcpServer,
		br:    br,
		sess:  sess,
		readr: readr,
		log:   logger,
	}

	tools.RegisterRead(mcpServer, readr)
	tools.RegisterLogin(mcpServer, br, sess, logger)
	tools.RegisterListSessions(mcpServer, sess)
	tools.RegisterClearSession(mcpServer, sess)
	tools.RegisterExtract(mcpServer, readr)

	return s, nil
}

// RunHTTP blocks and serves MCP over HTTP until ctx is cancelled. It exposes
// the streamable HTTP transport at /mcp, the legacy SSE transport at /sse,
// and a liveness probe at /healthz.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	getServer := func(*http.Request) *mcp.Server { return s.mcp }

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(getServer, nil))
	mux.Handle("/sse", mcp.NewSSEHandler(getServer, nil))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Close releases the browser and the session store. Safe to call multiple times.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		var firstErr error
		if s.br != nil {
			if err := s.br.Close(); err != nil {
				firstErr = err
				s.log.Printf("browser close: %v", err)
			}
		}
		if s.sess != nil {
			if err := s.sess.Close(); err != nil && firstErr == nil {
				firstErr = err
				s.log.Printf("session store close: %v", err)
			}
		}
		s.closeErr = firstErr
	})
	return s.closeErr
}
