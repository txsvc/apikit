// Package main provides a reference binary that exercises the apikit server
// with database initialization and optional admin bootstrap.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/auth"
	"github.com/txsvc/apikit/internal/bootstrap"
	"github.com/txsvc/apikit/internal/db"
	"github.com/txsvc/apikit/internal/handlers"
	"github.com/txsvc/apikit/internal/keys"
	"github.com/txsvc/apikit/internal/oauth"
)

func main() {
	adminEmail := flag.String("admin-email", "", "admin email for first-boot bootstrap")
	resetToken := flag.Bool("reset-admin-token", false, "rotate the admin token")
	flag.Parse()

	cfg, err := apikit.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	var bootstrapped bool
	database.SqlDB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM admin_config WHERE key = 'admin_token_hash')",
	).Scan(&bootstrapped)

	if *adminEmail != "" || *resetToken || bootstrapped {
		err := bootstrap.Run(context.Background(), bootstrap.BootstrapParams{
			DB:          database.SqlDB,
			AdminEmail:  *adminEmail,
			ResetToken:  *resetToken,
			ConfigDir:   apikit.ConfigDir(),
			TokenPrefix: apikit.TokenPrefix,
			Logger:      logrus.StandardLogger(),
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	checker := func() error {
		return database.Ping(context.Background())
	}
	server := apikit.NewServer(cfg, checker)

	// Build OAuth provider registry from config.
	oauthProviders := make([]oauth.ProviderConfig, len(cfg.OAuth.Providers))
	for i, p := range cfg.OAuth.Providers {
		oauthProviders[i] = oauth.ProviderConfig(p)
	}
	registry, err := oauth.BuildRegistryFromConfig(oauthProviders, http.DefaultClient)
	if err != nil {
		log.Fatal(err)
	}

	// Mount OAuth, auth middleware, and API handlers on the API group.
	api := server.APIGroup()
	oauth.RegisterOAuthHandlers(api, registry, database, cfg.Server.ExternalURL)

	permReg := auth.NewPermissionRegistry()
	api.Use(auth.NewAuthMiddleware(database, permReg))

	handlers.RegisterUserHandlers(api, database.SqlDB)
	handlers.RegisterOrgHandlers(api, database.SqlDB)
	keys.RegisterKeyHandlers(api, database.SqlDB)
	handlers.NewPATHandler(database, permReg).RegisterRoutes(api)

	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}
