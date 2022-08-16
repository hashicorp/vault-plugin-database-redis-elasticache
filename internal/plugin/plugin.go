package plugin

import (
	"os"

	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
)

// Verify interface is implemented
var _ dbplugin.Database = (*redisElastiCacheDB)(nil)

type redisElastiCacheDB struct {
	logger hclog.Logger
	config config
	client *elasticache.ElastiCache
}

type config struct {
	Username string `mapstructure:"username,omitempty"`
	Password string `mapstructure:"password,omitempty"`
	Url      string `mapstructure:"url,omitempty"`
	Region   string `mapstructure:"region,omitempty"`
}

func New() (dbplugin.Database, error) {
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
