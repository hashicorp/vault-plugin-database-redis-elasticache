package rediselasticache

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
)

type fields struct {
	logger hclog.Logger
	config config
	client *elasticache.ElastiCache
}

type args struct {
	ctx context.Context
	req interface{}
}

type testCases []struct {
	name    string
	fields  fields
	args    args
	want    interface{}
	wantErr bool
}

func skipIfAccTestNotEnabled(t *testing.T) {
	if _, ok := os.LookupEnv("ACC_TEST_ENABLED"); !ok {
		t.Skip(fmt.Printf("Skipping accpetance test %s; ACC_TEST_ENABLED is not set.", t.Name()))
	}
}

func setUpEnvironment() (fields, map[string]interface{}, redisElastiCacheDB, string) {
	accessKeyID := os.Getenv("TEST_ELASTICACHE_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("TEST_ELASTICACHE_SECRET_ACCESS_KEY")
	url := os.Getenv("TEST_ELASTICACHE_URL")
	region := os.Getenv("TEST_ELASTICACHE_REGION")
	user := os.Getenv("TEST_ELASTICACHE_USER")

	f := fields{
		logger: hclog.New(&hclog.LoggerOptions{
			Level:      hclog.Trace,
			Output:     os.Stderr,
			JSONFormat: true,
		}),
		config: config{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			Url:             url,
			Region:          region,
		},
		client: nil,
	}

	c := map[string]interface{}{
		"access_key_id":     accessKeyID,
		"secret_access_key": secretAccessKey,
		"url":               url,
		"region":            region,
	}

	r := redisElastiCacheDB{
		logger: f.logger,
		config: f.config,
		client: f.client,
	}

	return f, c, r, user
}

func setUpClient(t *testing.T, r *redisElastiCacheDB, config map[string]interface{}) {
	_, err := r.Initialize(nil, dbplugin.InitializeRequest{
		Config:           config,
		VerifyConnection: true,
	})
	if err != nil {
		t.Errorf("unable to pre initialize redis client for test cases: %v", err)
	}
}

func Test_redisElastiCacheDB_Initialize(t *testing.T) {
	f, c, r, _ := setUpEnvironment()
	skipIfAccTestNotEnabled(t)

	configWithDeprecatedFields := map[string]interface{}{
		"username": c["access_key_id"],
		"password": c["secret_access_key"],
		"url":      c["url"],
		"region":   c["region"],
	}

	tests := testCases{
		{
			name:   "initialize and verify connection succeeds",
			fields: f,
			args: args{
				req: dbplugin.InitializeRequest{
					Config:           c,
					VerifyConnection: true,
				},
			},
			want: dbplugin.InitializeResponse{
				Config: c,
			},
		},
		{
			name:   "initialize with deprecated attributes is valid",
			fields: f,
			args: args{
				req: dbplugin.InitializeRequest{
					Config:           configWithDeprecatedFields,
					VerifyConnection: true,
				},
			},
			want: dbplugin.InitializeResponse{
				Config: configWithDeprecatedFields,
			},
		},
		{
			name:   "initialize with invalid config fails",
			fields: f,
			args: args{
				req: dbplugin.InitializeRequest{
					Config: map[string]interface{}{
						"access_key_id":     "wrong",
						"secret_access_key": "wrong",
						"url":               "wrong",
						"region":            "wrong",
					},
					VerifyConnection: true,
				},
			},
			want:    dbplugin.InitializeResponse{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &r
			got, err := r.Initialize(tt.args.ctx, tt.args.req.(dbplugin.InitializeRequest))
			if (err != nil) != tt.wantErr {
				t.Errorf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Initialize() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_redisElastiCacheDB_UpdateUser(t *testing.T) {
	f, c, r, u := setUpEnvironment()

	skipIfAccTestNotEnabled(t)
	setUpClient(t, &r, c)

	tests := testCases{
		{
			name:   "update password of existing user succeeds",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.UpdateUserRequest{
					Username:       u,
					CredentialType: 0,
					Password: &dbplugin.ChangePassword{
						NewPassword: "abcdefghijklmnopqrstuvwxyz1",
					},
				},
			},
			want: dbplugin.UpdateUserResponse{},
		},
		{
			name:   "update password of non-existing user fails",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.UpdateUserRequest{
					Username:       "I do not exist",
					CredentialType: 0,
					Password: &dbplugin.ChangePassword{
						NewPassword: "abcdefghijklmnopqrstuvwxyz1",
					},
				},
			},
			want:    dbplugin.UpdateUserResponse{},
			wantErr: true,
		},
		{
			name:   "update to invalid password fails",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.UpdateUserRequest{
					Username:       u,
					CredentialType: 0,
					Password: &dbplugin.ChangePassword{
						NewPassword: "too short",
					},
				},
			},
			want:    dbplugin.UpdateUserResponse{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.UpdateUser(tt.args.ctx, tt.args.req.(dbplugin.UpdateUserRequest))
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateUser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpdateUser() got = %v, want %v", got, tt.want)
			}
		})
	}
}
