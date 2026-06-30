// Copyright IBM Corp. 2022, 2025
// SPDX-License-Identifier: MPL-2.0

package rediselasticache

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/hashicorp/go-hclog"
	awsutil "github.com/hashicorp/go-secure-stdlib/awsutil/v2"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
	"github.com/mitchellh/mapstructure"
)

// Verify interfaces are implemented
var _ dbplugin.Database = (*redisElastiCacheDB)(nil)
var _ elastiCacheAPI = (*elasticache.Client)(nil)

// elastiCacheAPI covers the two ElastiCache operations used by this plugin.
// AWS SDK v2 does not ship pre-generated iface packages; consumers define
// their own narrow interfaces to keep call-site surface small and testable.
type elastiCacheAPI interface {
	DescribeUsers(ctx context.Context, params *elasticache.DescribeUsersInput, optFns ...func(*elasticache.Options)) (*elasticache.DescribeUsersOutput, error)
	ModifyUser(ctx context.Context, params *elasticache.ModifyUserInput, optFns ...func(*elasticache.Options)) (*elasticache.ModifyUserOutput, error)
}

type redisElastiCacheDB struct {
	logger hclog.Logger
	config config
	client elastiCacheAPI
}

type config struct {
	AccessKeyID     string `mapstructure:"access_key_id,omitempty"`
	SecretAccessKey string `mapstructure:"secret_access_key,omitempty"`
	Url             string `mapstructure:"url,omitempty"`
	Region          string `mapstructure:"region,omitempty"`

	Username string `mapstructure:"username,omitempty"` // @Deprecated, use AccessKeyID instead
	Password string `mapstructure:"password,omitempty"` // @Deprecated, use SecretAccessKey instead
}

func (r *redisElastiCacheDB) Initialize(ctx context.Context, req dbplugin.InitializeRequest) (dbplugin.InitializeResponse, error) {
	r.logger.Debug("initializing AWS ElastiCache Redis client")

	if err := mapstructure.WeakDecode(req.Config, &r.config); err != nil {
		return dbplugin.InitializeResponse{}, err
	}

	// If primary connection attributes are not set, try to fall back on the deprecated values for backward compatibility
	accessKey := r.config.AccessKeyID
	if accessKey == "" && r.config.Username != "" {
		accessKey = r.config.Username
	}
	secretKey := r.config.SecretAccessKey
	if secretKey == "" && r.config.Password != "" {
		secretKey = r.config.Password
	}

	// GetRegion checks plugin config, env vars (AWS_REGION/AWS_DEFAULT_REGION), then
	// IMDS. In non-EC2 environments IMDS is unavailable and GetRegion returns an error;
	// fall back to DefaultRegion to preserve the v1 SDK contract where a missing region
	// never hard-failed initialisation.
	// TODO: remove this workaround once go-secure-stdlib/awsutil.GetRegion treats
	// IMDS unavailability as non-fatal and falls through to DefaultRegion itself.
	region, regionErr := awsutil.GetRegion(ctx, r.config.Region)
	if regionErr != nil {
		// Don't mask context cancellation or deadline as an IMDS failure.
		if ctx.Err() != nil {
			return dbplugin.InitializeResponse{}, ctx.Err()
		}
		region = awsutil.DefaultRegion
		r.logger.Debug("region resolution failed, using default region", "region", region, "error", regionErr)
	}
	// RetrieveCreds can produce url.Error from network calls in the provider
	// chain. Log full detail at debug; return a clean message to the operator.
	cfg, err := awsutil.RetrieveCreds(ctx, accessKey, secretKey, "", r.logger)
	if err != nil {
		r.logger.Debug("credential resolution failed", "error", err)
		return dbplugin.InitializeResponse{}, fmt.Errorf("unable to retrieve AWS credentials from provider chain")
	}

	// awsutil.RetrieveCreds does not set Region on the returned aws.Config.
	// Override it explicitly so the ElastiCache client resolves the correct
	// regional endpoint.
	cfg.Region = region

	r.client = elasticache.NewFromConfig(*cfg)

	if req.VerifyConnection {
		r.logger.Debug("Verifying credentials and region configuration via ElastiCache API")

		_, err := r.client.DescribeUsers(ctx, &elasticache.DescribeUsersInput{})
		if err != nil {
			// Use %s (not %w): SDK v2 errors implement Unwrap(), so errwrap can
			// traverse the chain and find url.Error, which DatabaseErrorSanitizerMiddleware
			// replaces with a generic message. Using %s breaks the chain and keeps
			// API-level errors (auth failures, bad endpoint) readable.
			r.logger.Debug("ElastiCache API verification failed", "region", region, "error", err)
			return dbplugin.InitializeResponse{}, fmt.Errorf("unable to verify ElastiCache API access (region %q): %s", region, err)
		}
	}

	return dbplugin.InitializeResponse{
		Config: req.Config,
	}, nil
}

func (r *redisElastiCacheDB) Type() (string, error) {
	return "redisElastiCache", nil
}

func (r *redisElastiCacheDB) Close() error {
	return nil
}

func (r *redisElastiCacheDB) NewUser(_ context.Context, _ dbplugin.NewUserRequest) (dbplugin.NewUserResponse, error) {
	return dbplugin.NewUserResponse{}, fmt.Errorf("user creation not supported")
}

func (r *redisElastiCacheDB) DeleteUser(_ context.Context, _ dbplugin.DeleteUserRequest) (dbplugin.DeleteUserResponse, error) {
	return dbplugin.DeleteUserResponse{}, fmt.Errorf("user deletion not supported")
}

func (r *redisElastiCacheDB) UpdateUser(ctx context.Context, req dbplugin.UpdateUserRequest) (dbplugin.UpdateUserResponse, error) {
	r.logger.Debug("updating AWS ElastiCache Redis user", "username", req.Username)

	out, err := r.client.DescribeUsers(ctx, &elasticache.DescribeUsersInput{
		UserId: aws.String(req.Username),
	})
	if err != nil {
		// Use %s (not %w): API errors (e.g. UserNotFoundFault) are preserved as a
		// readable string; network errors are also logged at debug.
		r.logger.Debug("user lookup failed", "username", req.Username, "error", err)
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("unable to get user %s: %s", req.Username, err)
	}
	if len(out.Users) == 1 && aws.ToString(out.Users[0].Status) != "active" {
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("user %s cannot be updated because it is not in the 'active' state", req.Username)
	}

	_, err = r.client.ModifyUser(ctx, &elasticache.ModifyUserInput{
		UserId:    aws.String(req.Username),
		Passwords: []string{req.Password.NewPassword},
	})
	if err != nil {
		// Use %s (not %w): password policy violations and other API errors are
		// preserved as readable strings for the operator to act on.
		r.logger.Debug("user update failed", "username", req.Username, "error", err)
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("unable to update user %s: %s", req.Username, err)
	}

	return dbplugin.UpdateUserResponse{}, nil
}
