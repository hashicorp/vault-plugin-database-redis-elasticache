package plugin

import (
	"context"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
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

func skipIfEnvIsUnset(t *testing.T, config config) {
	if config.Username == "" || config.Password == "" || config.Url == "" || config.Region == "" {
		t.Skip("Skipping acceptance tests because required environment variables are not configured")
	}
}

func setUpEnvironment() (fields, map[string]interface{}, redisElastiCacheDB) {
	username := os.Getenv("TEST_ELASTICACHE_USERNAME")
	password := os.Getenv("TEST_ELASTICACHE_PASSWORD")
	url := os.Getenv("TEST_ELASTICACHE_URL")
	region := os.Getenv("TEST_ELASTICACHE_REGION")

	f := fields{
		logger: hclog.New(&hclog.LoggerOptions{
			Level:      hclog.Trace,
			Output:     os.Stderr,
			JSONFormat: true,
		}),
		config: config{
			Username: username,
			Password: password,
			Url:      url,
			Region:   region,
		},
		client: nil,
	}

	c := map[string]interface{}{
		"username": username,
		"password": password,
		"url":      url,
		"region":   region,
	}

	r := redisElastiCacheDB{
		logger: f.logger,
		config: f.config,
		client: f.client,
	}

	return f, c, r
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

func setUpTestUser(t *testing.T, r *redisElastiCacheDB) string {
	user, err := r.NewUser(nil, dbplugin.NewUserRequest{
		UsernameConfig: dbplugin.UsernameMetadata{
			DisplayName: "display",
			RoleName:    "role",
		},
		Statements: dbplugin.Statements{
			Commands: []string{"on ~test* -@all +@read"},
		},
		Password: "abcdefghijklmnopqrstuvwxyz",
	})

	if err != nil {
		t.Errorf("unable to provision test user for test cases: %v", err)
	}

	return user.Username
}

func teardownTestUser(t *testing.T, r redisElastiCacheDB, username string) {
	if username == "" {
		return
	}

	// Creating or Modifying users cannot be deleted until they return to Active status
	for i := 0; i < 20; i++ {
		_, err := r.DeleteUser(nil, dbplugin.DeleteUserRequest{
			Username: username,
		})

		if err == nil {
			break
		} else {
			t.Logf("unable to clean test user '%s' due to: %v; retrying", username, err)
		}

		time.Sleep(3 * time.Second)
	}
}

func Test_redisElastiCacheDB_Initialize(t *testing.T) {
	f, c, r := setUpEnvironment()
	skipIfEnvIsUnset(t, f.config)

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
			name:   "initialize with invalid config fails",
			fields: f,
			args: args{
				req: dbplugin.InitializeRequest{
					Config: map[string]interface{}{
						"username": "wrong",
						"password": "wrong",
						"url":      "wrong",
						"region":   "wrong",
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

func Test_redisElastiCacheDB_NewUser(t *testing.T) {
	f, c, r := setUpEnvironment()

	skipIfEnvIsUnset(t, f.config)

	setUpClient(t, &r, c)

	tests := testCases{
		{
			name:   "create new valid user succeeds",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.NewUserRequest{
					UsernameConfig: dbplugin.UsernameMetadata{
						DisplayName: "display",
						RoleName:    "role",
					},
					Statements: dbplugin.Statements{
						Commands: []string{"on ~test* -@all +@read"},
					},
					Password: "abcdefghijklmnopqrstuvwxyz",
				},
			},
			want: dbplugin.NewUserResponse{
				Username: "v_displ_role_",
			},
		},
		{
			name:   "create new valid user from multiple commands",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.NewUserRequest{
					UsernameConfig: dbplugin.UsernameMetadata{
						DisplayName: "display",
						RoleName:    "role",
					},
					Statements: dbplugin.Statements{
						Commands: []string{"on", "~test*", "-@all", "+@read"},
					},
					Password: "abcdefghijklmnopqrstuvwxyz",
				},
			},
			want: dbplugin.NewUserResponse{
				Username: "v_displ_role_",
			},
		},
		{
			name:   "create user truncates username",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.NewUserRequest{
					UsernameConfig: dbplugin.UsernameMetadata{
						DisplayName: "iAmSupeExtremelyLongThisWillHaveToBeTruncated",
						RoleName:    "iAmEvenLongerTheApiWillDefinitelyRejectUsIfWeArePassedAsIsWithoutAnyModifications",
					},
					Statements: dbplugin.Statements{
						Commands: []string{"on ~test* -@all +@read"},
					},
					Password: "abcdefghijklmnopqrstuvwxyz",
				},
			},
			want: dbplugin.NewUserResponse{
				Username: "v_iAmSu_iAmEvenLongerTheApiWillDefinitelyRejec",
			},
		},
		{
			name:   "create user with invalid password fails",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.NewUserRequest{
					UsernameConfig: dbplugin.UsernameMetadata{
						DisplayName: "display",
						RoleName:    "role",
					},
					Statements: dbplugin.Statements{
						Commands: []string{"+@all"},
					},
					Password: "too short",
				},
			},
			want:    dbplugin.NewUserResponse{},
			wantErr: true,
		},
		{
			name:   "create user with invalid statements fails",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.NewUserRequest{
					UsernameConfig: dbplugin.UsernameMetadata{
						DisplayName: "display",
						RoleName:    "role",
					},
					Statements: dbplugin.Statements{
						Commands: []string{"+@invalid"},
					},
					Password: "abcdefghijklmnopqrstuvwxyz",
				},
			},
			want:    dbplugin.NewUserResponse{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.NewUser(tt.args.ctx, tt.args.req.(dbplugin.NewUserRequest))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewUser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !strings.HasPrefix(got.Username, tt.want.(dbplugin.NewUserResponse).Username) {
				t.Errorf("NewUser() got = %v, want %v", got, tt.want)
			}

			teardownTestUser(t, r, got.Username)
		})
	}
}

func Test_redisElastiCacheDB_UpdateUser(t *testing.T) {
	f, c, r := setUpEnvironment()

	skipIfEnvIsUnset(t, f.config)

	setUpClient(t, &r, c)
	username := setUpTestUser(t, &r)
	defer teardownTestUser(t, r, username)

	tests := testCases{
		{
			name:   "update password of existing user succeeds",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.UpdateUserRequest{
					Username:       username,
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
					Username:       username,
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

func Test_redisElastiCacheDB_DeleteUser(t *testing.T) {
	f, c, r := setUpEnvironment()

	skipIfEnvIsUnset(t, f.config)

	setUpClient(t, &r, c)
	username := setUpTestUser(t, &r)

	tests := testCases{
		{
			name:   "delete existing user succeeds",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.DeleteUserRequest{
					Username: username,
				},
			},
			want:    dbplugin.DeleteUserResponse{},
			wantErr: false,
		},
		{
			name:   "delete non-existing user fails",
			fields: f,
			args: args{
				ctx: context.Background(),
				req: dbplugin.DeleteUserRequest{
					Username: "I do not exist",
				},
			},
			want:    dbplugin.DeleteUserResponse{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.DeleteUser(tt.args.ctx, tt.args.req.(dbplugin.DeleteUserRequest))
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteUser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DeleteUser() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateUserId(t *testing.T) {
	type args struct {
		username string
	}

	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "compliant username",
			args: args{username: "isrole1234eEvyH4mEPcCIT4tCvE131660656371"},
			want: "isrole1234eEvyH4mEPcCIT4tCvE131660656371",
		},
		{
			name: "short username",
			args: args{username: "abcd"},
			want: "abcd",
		},
		{
			name: "username too long",
			args: args{username: "vtokenredisrole1234eEvyH4mEPcCIT4tCvE131660656371"},
			want: "isrole1234eEvyH4mEPcCIT4tCvE131660656371",
		},
		{
			name: "username with non-alphanumeric characters",
			args: args{username: "v_token_redis-role!/$}"},
			want: "vtokenredisrole",
		},
		{
			name: "username starting with a number",
			args: args{username: "1bcd"},
			want: "abcd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateUserId(tt.args.username); got != tt.want {
				t.Errorf("generateUserId() = %v, want %v", got, tt.want)
			}
		})
	}
}
