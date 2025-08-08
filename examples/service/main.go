package main

import (
	"context"
	"flag"
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
	// example of using a cmd line flag for configuration
	useTLS := flag.Bool("tls", false, "use TLS endpoint termination")
	flag.Parse()

	svc, err := apikit.New(setup, shutdown)
	if err != nil {
		log.Fatal(err)
	}

	if *useTLS {
		// start listening on port 443
		svc.ListenAutoTLS("")
	} else {
		// start listening on default port 8080
		svc.Listen("")
	}
}

func setup() *echo.Echo {
	// create a new router instance
	e := echo.New()

	// add and configure any middlewares
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.DefaultCORSConfig))

	// add common endpoints
	e = api.WithAuthEndpoints(e)

	// add your own endpoints here
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
