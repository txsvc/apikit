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
	"github.com/txsvc/apikit/auth"
	"github.com/txsvc/apikit/config"
	"github.com/txsvc/stdlib/v2/settings"
)

// the below version numbers should match the git release tags,
// i.e. there should be e.g. a version 'v0.1.0' on branch main !
const (
	// MajorVersion of the API
	majorVersion = 0
	// MinorVersion of the API
	minorVersion = 1
	// FixVersion of the API
	fixVersion = 0
)

type (
	appConfig struct {
		// the interface to implement
		config.ConfigProvider

		// some implementation specifc data
		info *config.Info
		ds   *settings.DialSettings
	}
)

func NewAppEngineConfigProvider() config.ConfigProvider {
	info := config.NewAppInfo(
		"appengine kit",
		"aek",
		"Copyright 2022, transformative.services, https://txs.vc",
		"about appengine kit",
		majorVersion,
		minorVersion,
		fixVersion,
	)

	return &appConfig{
		info: &info,
	}
}

func init() {
	// initialize the config provider
	config.SetProvider(config.NewLocalConfigProvider())

	// create a default configuration for the service (if none exists)
	path := filepath.Join(config.GetConfig().ConfigLocation(), config.DefaultConfigName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			// Handle error appropriately for your application
			return
		}

		// create credentials and keys with defaults from this config provider
		cfg := config.GetConfig().Settings()

		// save the new configuration
		if err := settings.WriteDialSettings(cfg, path); err != nil {
			// Handle error appropriately for your application
			return
		}
	}
}

func main() {

	svc, err := apikit.New(setup, shutdown)
	if err != nil {
		log.Fatal(err)
	}

	// Do not use AutoTLS here as TLS termination is handled by Google App Engine.
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
	e.GET("/", api.DefaultEndpoint)
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
		Message: fmt.Sprintf("version: %s", config.GetConfig().Info().VersionString()),
	}

	return api.StandardResponse(c, http.StatusOK, resp)
}
