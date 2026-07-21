package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/txsvc/apikit"
)

func main() {
	adminEmail := flag.String("admin-email", "", "admin email for first-boot bootstrap")
	resetToken := flag.Bool("reset-admin-token", false, "rotate the admin token")
	flag.Parse()

	cfg, err := apikit.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	database, err := apikit.OpenDatabase(cfg.Database.Path)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	if err := apikit.Bootstrap(context.Background(), database, apikit.BootstrapOptions{
		AdminEmail: *adminEmail,
		ResetToken: *resetToken,
	}); err != nil {
		if errors.Is(err, apikit.ErrBootstrapComplete) {
			fmt.Println(err)
			return
		}
		log.Fatal(err)
	}

	server := apikit.NewServer(cfg, func() error {
		return database.Ping(context.Background())
	})

	if err := server.MountHandlers(database); err != nil {
		log.Fatal(err)
	}

	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}
