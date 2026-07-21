package apikit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/handlers"
	"github.com/txsvc/apikit/internal/keys"
	"github.com/txsvc/apikit/internal/oauth"
)

// drainTimeout is the fixed drain window for graceful shutdown.
// It is not user-configurable.
const drainTimeout = 15 * time.Second

// HealthChecker is a function that checks service health.
// A nil HealthChecker means the service is always considered ready.
type HealthChecker func() error

// Server wraps an Echo HTTP server with apikit configuration.
type Server struct {
	cfg      *Config
	checker  HealthChecker
	echo     *echo.Echo
	once     sync.Once
	addr     string
	addrMu   sync.RWMutex
	listener net.Listener
	apiGroup *echo.Group
	shutdown bool
	done     chan struct{}
}

// NewServer constructs a configured Echo HTTP server from a *Config.
// It panics if cfg is nil (programming error).
// It does not perform file I/O or call LoadConfig().
func NewServer(cfg *Config, checker HealthChecker) *Server {
	if cfg == nil {
		panic("apikit.NewServer: cfg must not be nil")
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Set the custom HTTP error handler for standard JSON envelopes
	e.HTTPErrorHandler = HTTPErrorHandler

	// Configure logrus
	logrus.SetFormatter(&logrus.JSONFormatter{})
	level := strings.ToLower(cfg.Logging.Level)
	if level == "" {
		level = "info"
	}
	parsed, err := logrus.ParseLevel(level)
	if err != nil {
		parsed = logrus.InfoLevel
	}
	logrus.SetLevel(parsed)

	s := &Server{
		cfg:     cfg,
		checker: checker,
		echo:    e,
	}

	// Register middleware in the corrected order. The critical reviewer finding
	// identified that the spec's original order placed Logging after Body Size
	// Limit and Content-Type Enforcement, which meant Logging was never reached
	// when those middleware short-circuited. Additionally, Security Headers must
	// run before any middleware that can short-circuit, to ensure security headers
	// appear on EVERY response (01-PROP-4).
	//
	// Corrected order:
	// (1) Panic Recovery  — outermost; catches panics from everything
	// (2) Request ID      — assigns UUID v4 early for logging and responses
	// (3) Security Headers — sets response headers before any short-circuit
	// (4) Logging          — wraps error-producing middleware to capture all status codes
	// (5) Body Size Limit  — may short-circuit with 413
	// (6) Content-Type Enforcement — may short-circuit with 415
	e.Use(panicRecoveryMiddleware())
	e.Use(requestIDMiddleware())
	e.Use(securityHeadersMiddleware())
	e.Use(loggingMiddleware())

	// Determine max body bytes: use parsed value from config, default to 1MB
	maxBytes := cfg.Server.MaxBodyBytes()
	if maxBytes <= 0 {
		// If MaxBodySize string is set but not parsed (e.g. config built directly),
		// try to parse it; otherwise default to 1MB
		if cfg.Server.MaxBodySize != "" {
			maxBytes = parseMaxBodySize(cfg.Server.MaxBodySize)
		}
		if maxBytes <= 0 {
			maxBytes = 1048576 // 1MB default
		}
	}
	e.Use(bodySizeLimitMiddleware(maxBytes))
	e.Use(contentTypeEnforcementMiddleware())

	// Register health probe endpoints at the server root (not under mount_point)
	e.GET("/healthz", s.healthzHandler, CacheMiddleware(CacheNoCache))
	e.GET("/readyz", s.readyzHandler, CacheMiddleware(CacheNoCache))
	e.GET("/version", s.versionHandler, CacheMiddleware(CachePublic))

	// Pre-create the API group with CacheNoStore applied
	mp := cfg.Server.MountPoint
	if mp == "" {
		mp = "/api/v1"
	}
	s.apiGroup = e.Group(mp, CacheMiddleware(CacheNoStore))

	return s
}

// parseMaxBodySize parses a size string like "1MB", "512KB", "2GB" to bytes.
// Returns 0 on failure.
func parseMaxBodySize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var suffix string
	var numStr string
	for i, c := range s {
		if c < '0' || c > '9' {
			numStr = s[:i]
			suffix = s[i:]
			break
		}
	}
	if numStr == "" {
		return 0
	}
	var num int64
	for _, c := range numStr {
		num = num*10 + int64(c-'0')
	}
	if num <= 0 {
		return 0
	}
	switch strings.ToUpper(suffix) {
	case "KB":
		return num * 1024
	case "MB":
		return num * 1024 * 1024
	case "GB":
		return num * 1024 * 1024 * 1024
	default:
		return 0
	}
}

// healthzHandler handles GET /healthz — liveness probe.
func (s *Server) healthzHandler(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// readyzHandler handles GET /readyz — readiness probe.
func (s *Server) readyzHandler(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.checker != nil {
		if err := s.checker(); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		}
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}

// versionHandler handles GET /version — server version information.
func (s *Server) versionHandler(c echo.Context) error {
	mp := s.cfg.Server.MountPoint
	if mp == "" {
		mp = "/api/v1"
	}
	c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	return c.JSON(http.StatusOK, map[string]string{
		"version":     Version,
		"build":       Build,
		"mount_point": mp,
	})
}

// Start binds the listener and serves requests until shutdown.
// It blocks until Shutdown() is called or a SIGTERM/SIGINT is received.
// Returns nil on clean shutdown or an error if binding fails.
func (s *Server) Start() error {
	// Prevent re-start after shutdown
	s.addrMu.RLock()
	wasShutdown := s.shutdown
	s.addrMu.RUnlock()
	if wasShutdown {
		return errors.New("server already shut down")
	}

	// Determine bind address
	bind := s.cfg.Server.Bind
	if bind == "" {
		bind = "0.0.0.0"
	}
	port := s.cfg.Server.Port

	addr := fmt.Sprintf("%s:%d", bind, port)

	// Create the listener manually to support ephemeral port (port=0)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln

	// Store the actual bound address
	s.addrMu.Lock()
	s.addr = ln.Addr().String()
	s.addrMu.Unlock()

	// Set up SIGTERM/SIGINT handler
	s.done = make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			s.Shutdown(context.Background())
		case <-s.done:
		}
		signal.Stop(sigCh)
	}()

	cfgPath, _ := filepath.Abs(ConfigPath())
	dbDir, _ := filepath.Abs(filepath.Dir(s.cfg.Database.Path))
	logrus.WithFields(logrus.Fields{
		"config": cfgPath,
		"data":   dbDir,
	}).Info("paths")
	logrus.WithFields(logrus.Fields{
		"addr":        s.addr,
		"mount_point": s.cfg.Server.MountPoint,
	}).Info("server listening")

	// Serve using the listener
	s.echo.Listener = ln
	err = s.echo.Start("")
	if err != nil && errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	close(s.done)

	// Clear the address after shutdown
	s.addrMu.Lock()
	s.addr = ""
	s.addrMu.Unlock()

	return err
}

// Shutdown initiates graceful server shutdown.
// Uses sync.Once to ensure one-time execution.
// Derives a context with drainTimeout (15 seconds) and shuts down the Echo server.
// All concurrent/subsequent calls return nil immediately.
func (s *Server) Shutdown(ctx context.Context) error {
	s.once.Do(func() {
		logrus.WithFields(logrus.Fields{
			"drain_timeout": fmt.Sprintf("%v", drainTimeout),
		}).Info("initiating shutdown with 15s drain timeout")

		// Mark as shut down
		s.addrMu.Lock()
		s.shutdown = true
		s.addrMu.Unlock()

		// Create a timeout context using the minimum of caller's context and drainTimeout
		shutdownCtx, cancel := context.WithTimeout(ctx, drainTimeout)
		defer cancel()

		_ = s.echo.Shutdown(shutdownCtx)
	})
	return nil
}

// Addr returns the actual bound host:port string after Start() begins listening.
// Returns empty string before Start() or after shutdown.
func (s *Server) Addr() string {
	s.addrMu.RLock()
	defer s.addrMu.RUnlock()
	return s.addr
}

// APIGroup returns the Echo route group at the configured mount point.
// The group has CacheMiddleware(CacheNoStore) pre-applied.
// Returns the same *echo.Group on every call.
func (s *Server) APIGroup() *echo.Group {
	return s.apiGroup
}

// MountHandlers registers the default OAuth, authentication, and API handlers
// on the server's API group. This wires up the OAuth provider registry from
// config, the auth middleware, and all resource handlers (users, orgs, keys, PATs).
//
// Must be called after NewServer and before Start.
func (s *Server) MountHandlers(database *DB, permissions ...Permission) error {
	oauthProviders := make([]oauth.ProviderConfig, len(s.cfg.OAuth.Providers))
	for i, p := range s.cfg.OAuth.Providers {
		oauthProviders[i] = oauth.ProviderConfig(p)
	}
	registry, err := oauth.BuildRegistryFromConfig(oauthProviders, http.DefaultClient)
	if err != nil {
		return err
	}

	api := s.APIGroup()
	oauth.RegisterOAuthHandlers(api, registry, database, s.cfg.Server.ExternalURL)

	permReg := auth.NewPermissionRegistry()
	for _, p := range permissions {
		if err := permReg.Register(p.Resource, p.Action); err != nil {
			return fmt.Errorf("registering permission %s:%s: %w", p.Resource, p.Action, err)
		}
	}
	api.Use(auth.NewAuthMiddleware(database, permReg))

	handlers.RegisterUserHandlers(api, database.SqlDB)
	handlers.RegisterOrgHandlers(api, database.SqlDB)
	keys.RegisterKeyHandlers(api, database.SqlDB)
	handlers.NewPATHandler(database, permReg).RegisterRoutes(api)

	return nil
}
