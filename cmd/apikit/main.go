// Package main provides a minimal reference binary that exercises the
// three-step apikit caller usage contract: LoadConfig, NewServer, Start.
package main

import (
	"log"

	"github.com/txsvc/apikit"
)

func main() {
	cfg, err := apikit.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	server := apikit.NewServer(cfg, nil)

	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}
