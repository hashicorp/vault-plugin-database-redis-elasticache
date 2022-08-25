package rediselasticache

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/vault/sdk/database/dbplugin/v5"
	"github.com/hashicorp/vault/sdk/database/helper/credsutil"
	"github.com/mitchellh/mapstructure"
)

var (
	nonAlphanumericHyphenRegex = regexp.MustCompile("[^a-zA-Z\\d-]+")
	doubleHyphenRegex          = regexp.MustCompile("-{2,}")
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

func (r *redisElastiCacheDB) Initialize(_ context.Context, req dbplugin.InitializeRequest) (dbplugin.InitializeResponse, error) {
	r.logger.Debug("initializing AWS ElastiCache Redis client")

	if err := mapstructure.WeakDecode(req.Config, &r.config); err != nil {
		return dbplugin.InitializeResponse{}, err
	}

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(r.config.Region),
		Credentials: credentials.NewStaticCredentials(r.config.Username, r.config.Password, ""),
	})
	if err != nil {
		return dbplugin.InitializeResponse{}, fmt.Errorf("unable to initialize AWS session: %w", err)
	}
	r.client = elasticache.New(sess)

	if req.VerifyConnection {
		r.logger.Debug("Verifying connection to instance", "url", r.config.Url)

		_, err := r.client.DescribeUsers(nil)
		if err != nil {
			return dbplugin.InitializeResponse{}, fmt.Errorf("unable to connect to ElastiCache Redis endpoint: %w", err)
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

func (r *redisElastiCacheDB) NewUser(_ context.Context, req dbplugin.NewUserRequest) (dbplugin.NewUserResponse, error) {
	r.logger.Debug("creating new AWS ElastiCache Redis user", "role", req.UsernameConfig.RoleName)

	// Format: v_{displayName}_{roleName}_{ID[20]}_{epoch[11]}
	// Length limits set so unique identifiers are not truncated
	username, err := credsutil.GenerateUsername(
		credsutil.DisplayName(req.UsernameConfig.DisplayName, 5),
		credsutil.RoleName(req.UsernameConfig.RoleName, 39),
		credsutil.MaxLength(80),
	)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to generate username: %w", err)
	}

	accessString, err := parseCreationCommands(req.Statements.Commands)
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to parse acess string: %w", err)
	}

	userId := normaliseId(username)

	output, err := r.client.CreateUser(&elasticache.CreateUserInput{
		AccessString:       aws.String(accessString),
		Engine:             aws.String("Redis"),
		NoPasswordRequired: aws.Bool(false),
		Passwords:          []*string{&req.Password},
		Tags:               []*elasticache.Tag{},
		UserId:             aws.String(userId),
		UserName:           aws.String(username),
	})
	if err != nil {
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to create new user: %w", err)
	}

	return dbplugin.NewUserResponse{Username: *output.UserName}, nil
}

func (r *redisElastiCacheDB) UpdateUser(_ context.Context, req dbplugin.UpdateUserRequest) (dbplugin.UpdateUserResponse, error) {
	r.logger.Debug("updating AWS ElastiCache Redis user", "username", req.Username)

	userId := normaliseId(req.Username)

	_, err := r.client.ModifyUser(&elasticache.ModifyUserInput{
		UserId:    &userId,
		Passwords: []*string{&req.Password.NewPassword},
	})
	if err != nil {
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("unable to update user: %w", err)
	}

	return dbplugin.UpdateUserResponse{}, nil
}

func (r *redisElastiCacheDB) DeleteUser(_ context.Context, req dbplugin.DeleteUserRequest) (dbplugin.DeleteUserResponse, error) {
	r.logger.Debug("deleting AWS ElastiCache Redis user", "username", req.Username)

	userId := normaliseId(req.Username)

	_, err := r.client.DeleteUser(&elasticache.DeleteUserInput{
		UserId: &userId,
	})
	if err != nil {
		return dbplugin.DeleteUserResponse{}, fmt.Errorf("unable to delete user: %w", err)
	}

	return dbplugin.DeleteUserResponse{}, nil
}

func parseCreationCommands(commands []string) (string, error) {
	if len(commands) == 0 {
		return "on ~* +@read", nil
	}

	accessString := ""
	for _, command := range commands {
		var rules []string
		err := json.Unmarshal([]byte(command), &rules)
		if err != nil {
			return "", err
		}

		accessString += strings.Join(rules, " ")
		accessString += " "
	}

	if !(strings.HasPrefix(accessString, "on ") || strings.Contains(accessString, " on ") || strings.HasSuffix(accessString, " on")) {
		accessString = "on " + accessString
	}

	accessString = strings.TrimSpace(accessString)

	return accessString, nil
}

// All Elasticache IDs can have up to 40 characters, and must begin with a letter.
// It should not end with a hyphen or contain two consecutive hyphens.
// Valid characters: A-Z, a-z, 0-9, and -(hyphen).
func normaliseId(raw string) string {
	normalized := nonAlphanumericHyphenRegex.ReplaceAllString(raw, "")
	normalized = doubleHyphenRegex.ReplaceAllString(normalized, "")

	if len(normalized) > 40 {
		normalized = normalized[len(normalized)-40:]
	}

	if unicode.IsNumber(rune(normalized[0])) {
		normalized = string(rune('A'-17+normalized[0])) + normalized[1:]
	}

	if strings.HasSuffix(normalized, "-") {
		normalized = normalized[:len(normalized)-1] + "x"
	}

	return normalized
}
