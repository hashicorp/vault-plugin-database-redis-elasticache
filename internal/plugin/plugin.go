package plugin

import (
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
)

func New() (interface{}, error) {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})

	db := &redisElastiCacheDB{
		logger: logger,
	}

	return wrapWithSanitizerMiddleware(db), nil
}

func wrapWithSanitizerMiddleware(db *redisElastiCacheDB) dbplugin.Database {
	return dbplugin.NewDatabaseErrorSanitizerMiddleware(db, db.secretValuesToMask)
}

func (r *redisElastiCacheDB) secretValuesToMask() map[string]string {
	return map[string]string{
		r.config.Password: "[password]",
		r.config.Username: "[username]",
	}
}
