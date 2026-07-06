// Copyright IBM Corp. 2022, 2025
// SPDX-License-Identifier: MPL-2.0

package rediselasticache

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/hashicorp/go-hclog"
	awsutil "github.com/hashicorp/go-secure-stdlib/awsutil/v2"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
)

type fields struct {
	logger hclog.Logger
	config config
	client elastiCacheAPI
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

// mockElastiCacheClient implements elastiCacheAPI for unit tests.
type mockElastiCacheClient struct {
	describeUsersOutput *elasticache.DescribeUsersOutput
	describeUsersErr    error
	modifyUserOutput    *elasticache.ModifyUserOutput
	modifyUserErr       error
}

func (m *mockElastiCacheClient) DescribeUsers(_ context.Context, _ *elasticache.DescribeUsersInput, _ ...func(*elasticache.Options)) (*elasticache.DescribeUsersOutput, error) {
	return m.describeUsersOutput, m.describeUsersErr
}

func (m *mockElastiCacheClient) ModifyUser(_ context.Context, _ *elasticache.ModifyUserInput, _ ...func(*elasticache.Options)) (*elasticache.ModifyUserOutput, error) {
	return m.modifyUserOutput, m.modifyUserErr
}

func skipIfAccTestNotEnabled(t *testing.T) {
	t.Helper()
	if _, ok := os.LookupEnv("ACC_TEST_ENABLED"); !ok {
		t.Skipf("Skipping acceptance test %s; ACC_TEST_ENABLED is not set.", t.Name())
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
		client: nil,
	}

	return f, c, r, user
}

func setUpClient(t *testing.T, r *redisElastiCacheDB, config map[string]interface{}) {
	t.Helper()
	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           config,
		VerifyConnection: true,
	})
	if err != nil {
		t.Fatalf("unable to pre initialize redis client for test cases: %v", err)
	}
}

// Test_redisElastiCacheDB_Initialize_NoRegion verifies that Initialize with no
// region and verify_connection=false succeeds, matching the pre-v2 (v1 SDK)
// behaviour where a missing region never hard-failed initialisation.
func Test_redisElastiCacheDB_Initialize_NoRegion(t *testing.T) {
	// Isolate from any ambient AWS configuration on the test machine or CI runner.
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/nonexistent")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"access_key_id":     "someaccesskey",
		"secret_access_key": "somesecretkey",
		"url":               "some-cache.abc.cfg.use1.cache.amazonaws.com",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with no region should not fail: %v", err)
	}

	// Assert the client is configured with the documented fallback region.
	ec, ok := r.client.(*elasticache.Client)
	if !ok {
		t.Fatal("expected client to be *elasticache.Client")
	}
	if got := ec.Options().Region; got != awsutil.DefaultRegion {
		t.Fatalf("expected client region %q, got %q", awsutil.DefaultRegion, got)
	}
}

// Test_redisElastiCacheDB_Initialize_ExplicitCredsWithDefaultAWSProfile verifies
// that Initialize succeeds when explicit credentials are provided alongside a
// [default] profile in ~/.aws/config. awsutil/v2 defaults
// withSharedCredentials=true, which injects an empty WithSharedCredentialsFiles("")
// override; when a [default] profile also exists, LoadDefaultConfig attempts to
// load that profile's credentials from the empty path and fails. Passing
// WithSharedCredentials(false) unconditionally prevents this interference and
// lets the SDK run its own credential chain without a double-probe.
func Test_redisElastiCacheDB_Initialize_ExplicitCredsWithDefaultAWSProfile(t *testing.T) {
	// Write a temp AWS config file that contains a [default] profile — this is
	// the environmental condition that triggered the bug.
	configFile := t.TempDir() + "/config"
	if err := os.WriteFile(configFile, []byte("[default]\nregion = us-west-2\n"), 0o600); err != nil {
		t.Fatalf("failed to write temp AWS config file: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", configFile)
	// Point credentials file at a non-existent path — awsutil was passing an
	// empty file path, causing LoadDefaultConfig to attempt loading the [default]
	// profile's credentials from that empty path and fail.
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"access_key_id":     "someaccesskey",
		"secret_access_key": "somesecretkey",
		"url":               "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region":            "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with explicit creds and [default] AWS profile should not fail: %v", err)
	}

	ec, ok := r.client.(*elasticache.Client)
	if !ok {
		t.Fatal("expected client to be *elasticache.Client")
	}
	creds, err := ec.Options().Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatalf("failed to retrieve credentials from client: %v", err)
	}
	if creds.AccessKeyID != "someaccesskey" {
		t.Fatalf("expected static credentials from plugin config (AccessKeyID=%q), got %q", "someaccesskey", creds.AccessKeyID)
	}
}

// Test_redisElastiCacheDB_Initialize_SharedCredentialsFile verifies that
// Initialize succeeds when no explicit credentials are given and credentials
// come from a shared credentials file (~/.aws/credentials). This exercises
// the documented fallback: "If omitted, authentication falls back on the AWS
// credentials provider chain."
func Test_redisElastiCacheDB_Initialize_SharedCredentialsFile(t *testing.T) {
	credsFile := t.TempDir() + "/credentials"
	content := "[default]\naws_access_key_id = someaccesskey\naws_secret_access_key = somesecretkey\n"
	if err := os.WriteFile(credsFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp credentials file: %v", err)
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"url":    "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region": "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with shared credentials file should not fail: %v", err)
	}

	ec, ok := r.client.(*elasticache.Client)
	if !ok {
		t.Fatal("expected client to be *elasticache.Client")
	}
	creds, err := ec.Options().Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatalf("failed to retrieve credentials from client: %v", err)
	}
	if creds.AccessKeyID != "someaccesskey" {
		t.Fatalf("expected credentials from shared credentials file (AccessKeyID=%q), got %q", "someaccesskey", creds.AccessKeyID)
	}
}

// Test_redisElastiCacheDB_Initialize_EnvVarCredentials verifies that Initialize
// succeeds when no explicit credentials are set in the plugin config but
// AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are present in the environment.
// This exercises the SDK default credential chain path (accessKey == "").
func Test_redisElastiCacheDB_Initialize_EnvVarCredentials(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "envkeyid")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "envsecretkey")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"url":    "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region": "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with env var credentials should not fail: %v", err)
	}

	ec, ok := r.client.(*elasticache.Client)
	if !ok {
		t.Fatal("expected client to be *elasticache.Client")
	}
	creds, err := ec.Options().Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatalf("failed to retrieve credentials from client: %v", err)
	}
	if creds.AccessKeyID != "envkeyid" {
		t.Fatalf("expected credentials from env vars (AccessKeyID=%q), got %q", "envkeyid", creds.AccessKeyID)
	}
}

// Test_redisElastiCacheDB_Initialize_DeprecatedCredentials verifies that the
// deprecated username/password fields fall back correctly when access_key_id and
// secret_access_key are absent.
func Test_redisElastiCacheDB_Initialize_DeprecatedCredentials(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"username": "someaccesskey",
		"password": "somesecretkey",
		"url":      "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region":   "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with deprecated username/password should not fail: %v", err)
	}
}

// Test_redisElastiCacheDB_Initialize_NoCredentials verifies that Initialize fails
// with a clear error when no credentials are available from any source.
func Test_redisElastiCacheDB_Initialize_NoCredentials(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_ROLE_ARN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"url":    "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region": "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err == nil {
		t.Fatal("Initialize() with no credentials should fail")
	}
	if !strings.Contains(err.Error(), "unable to retrieve AWS credentials from provider chain") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// Test_redisElastiCacheDB_Initialize_StaticCredsOverrideSharedCredentialsFile
// verifies that when both a real AWS credentials file and explicit static keys
// in the plugin config are present, the static keys win. This covers the exact
// scenario reported as a bug: providing ~/.aws/credentials alongside
// access_key_id / secret_access_key in the database configuration.
// awsutil.WithSharedCredentials(false) prevents the SDK's credentials-file
// provider from interfering with the explicit static-credentials provider.
func Test_redisElastiCacheDB_Initialize_StaticCredsOverrideSharedCredentialsFile(t *testing.T) {
	// Write a credentials file with a [default] profile using DIFFERENT keys to
	// make it visible if they are accidentally used instead of the static keys.
	credsFile := t.TempDir() + "/credentials"
	content := "[default]\naws_access_key_id = fileaccesskey\naws_secret_access_key = filesecretkey\n"
	if err := os.WriteFile(credsFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp credentials file: %v", err)
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/nonexistent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	r := &redisElastiCacheDB{
		logger: hclog.NewNullLogger(),
	}

	cfg := map[string]interface{}{
		"access_key_id":     "staticaccesskey",
		"secret_access_key": "staticsecretkey",
		"url":               "some-cache.abc.cfg.use1.cache.amazonaws.com",
		"region":            "us-east-1",
	}

	_, err := r.Initialize(t.Context(), dbplugin.InitializeRequest{
		Config:           cfg,
		VerifyConnection: false,
	})
	if err != nil {
		t.Fatalf("Initialize() with static creds and a credentials file should not fail: %v", err)
	}

	ec, ok := r.client.(*elasticache.Client)
	if !ok {
		t.Fatal("expected client to be *elasticache.Client")
	}
	creds, err := ec.Options().Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatalf("failed to retrieve credentials from client: %v", err)
	}
	if creds.AccessKeyID != "staticaccesskey" {
		t.Fatalf("expected static credentials to take precedence (AccessKeyID=%q), got %q", "staticaccesskey", creds.AccessKeyID)
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
				ctx: t.Context(),
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
				ctx: t.Context(),
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
				ctx: t.Context(),
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
				ctx: t.Context(),
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
				ctx: t.Context(),
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
				ctx: t.Context(),
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

func Test_redisElastiCacheDB_UpdateUser_errorPaths(t *testing.T) {
	logger := hclog.NewNullLogger()

	activeUser := []elasticachetypes.User{{Status: aws.String("active")}}
	modifyingUser := []elasticachetypes.User{{Status: aws.String("modifying")}}

	tests := []struct {
		name    string
		client  *mockElastiCacheClient
		req     dbplugin.UpdateUserRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "DescribeUsers error returns formatted error",
			client: &mockElastiCacheClient{
				describeUsersErr: errors.New("api error InternalFailure"),
			},
			req: dbplugin.UpdateUserRequest{
				Username: "testuser",
				Password: &dbplugin.ChangePassword{NewPassword: "newpassword123"},
			},
			wantErr: true,
			errMsg:  "unable to get user testuser",
		},
		{
			name: "user not in active state returns error",
			client: &mockElastiCacheClient{
				describeUsersOutput: &elasticache.DescribeUsersOutput{Users: modifyingUser},
			},
			req: dbplugin.UpdateUserRequest{
				Username: "testuser",
				Password: &dbplugin.ChangePassword{NewPassword: "newpassword123"},
			},
			wantErr: true,
			errMsg:  "not in the 'active' state",
		},
		{
			name: "ModifyUser error returns formatted error",
			client: &mockElastiCacheClient{
				describeUsersOutput: &elasticache.DescribeUsersOutput{Users: activeUser},
				modifyUserErr:       errors.New("InvalidPasswordException"),
			},
			req: dbplugin.UpdateUserRequest{
				Username: "testuser",
				Password: &dbplugin.ChangePassword{NewPassword: "newpassword123"},
			},
			wantErr: true,
			errMsg:  "unable to update user testuser",
		},
		{
			name: "success when user is active and ModifyUser succeeds",
			client: &mockElastiCacheClient{
				describeUsersOutput: &elasticache.DescribeUsersOutput{Users: activeUser},
				modifyUserOutput:    &elasticache.ModifyUserOutput{},
			},
			req: dbplugin.UpdateUserRequest{
				Username: "testuser",
				Password: &dbplugin.ChangePassword{NewPassword: "newpassword123"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &redisElastiCacheDB{
				logger: logger,
				client: tt.client,
			}
			_, err := r.UpdateUser(t.Context(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateUser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("UpdateUser() error = %q, want error containing %q", err.Error(), tt.errMsg)
			}
		})
	}
}
