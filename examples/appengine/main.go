package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/api"
	"github.com/txsvc/apikit/config"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/settings"
)

// FIXME: implement in memory certstore

func init() {
	// initialize the config provider
	config.InitConfigProvider(config.NewSimpleConfigProvider())

	// create a default configuration for the service (if none exists)
	path := filepath.Join(config.ResolveConfigLocation(), config.DefaultConfigFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(path), os.ModePerm)

		// create credentials and keys with defaults from this config provider
		cfg := config.GetDefaultSettings()

		// save the new configuration
		settings.WriteSettingsToFile(cfg, path)
	}

	// initialize the credentials store
	root := filepath.Join(config.ResolveConfigLocation(), "cred")
	auth.FlushAuthorizations(root)
}

func main() {

	svc, err := apikit.New(setup, shutdown)
	if err != nil {
		log.Fatal(err)
	}

	// Do not use AutoTLS here as TLS termination is handled by App Engine.
	// Do not change default port 8080 !
	svc.Listen("")
}

func setup() *echo.Echo {
	// create a new router instance
	e := echo.New()

	// add and configure any middlewares
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.DefaultCORSConfig))

	// add your endpoints here
	e.GET("/ping", pingEndpoint)

	// done
	return e
}

func shutdown(ctx context.Context, a *apikit.App) error {
	// TODO: implement your own stuff here
	return nil
}

// pingEndpoint returns http.StatusOK and the version string
func pingEndpoint(c echo.Context) error {
	ctx := context.Background()

	// this endpoint needs at minimum an "api:read" scope
	_, err := auth.CheckAuthorization(ctx, c, auth.ScopeApiRead)
	if err != nil {
		return api.ErrorResponse(c, http.StatusUnauthorized, err, "")
	}

	resp := api.StatusObject{
		Status:  http.StatusOK,
		Message: fmt.Sprintf("version: %s", config.VersionString()),
	}

	return api.StandardResponse(c, http.StatusOK, resp)
}
