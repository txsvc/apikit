package apikit

import (
	"context"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// drainTimeout is the fixed drain window for graceful shutdown.
// It is not user-configurable.
const drainTimeout = 15 * time.Second

// HealthChecker is a function that checks service health.
// A nil HealthChecker means the service is always considered ready.
type HealthChecker func() error

// Server wraps an Echo HTTP server with apikit configuration.
type Server struct {
	cfg     *Config
	checker HealthChecker
	echo    *echo.Echo
	once    sync.Once
}

// NewServer constructs a configured Echo HTTP server from a *Config.
// It panics if cfg is nil (programming error).
// It does not perform file I/O or call LoadConfig().
func NewServer(cfg *Config, checker HealthChecker) *Server {
	if cfg == nil {
		panic("apikit.NewServer: cfg must not be nil")
	}
	return &Server{
		cfg:     cfg,
		checker: checker,
		echo:    echo.New(),
	}
}

// Start binds the listener and serves requests until shutdown.
func (s *Server) Start() error {
	return nil // stub
}

// Shutdown initiates graceful server shutdown.
func (s *Server) Shutdown(ctx context.Context) error {
	return nil // stub
}

// Addr returns the actual bound host:port string after Start() begins listening.
// Returns empty string before Start() or after shutdown.
func (s *Server) Addr() string {
	return "" // stub
}

// APIGroup returns the Echo route group at the configured mount point.
func (s *Server) APIGroup() *echo.Group {
	if s.echo == nil || s.cfg == nil {
		return nil
	}
	mp := s.cfg.Server.MountPoint
	if mp == "" {
		mp = "/api/v1"
	}
	return s.echo.Group(mp)
}
