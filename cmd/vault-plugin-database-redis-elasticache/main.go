package main

import (
	"log"
	"os"

	"github.com/hashicorp/vault-plugin-database-redis-elasticache/internal/plugin"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
)

func main() {
	if err := Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

// Run starts serving the plugin
func Run() error {
	dbplugin.ServeMultiplex(plugin.New)

	return nil
}