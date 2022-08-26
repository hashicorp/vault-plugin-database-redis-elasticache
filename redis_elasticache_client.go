package rediselasticache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to parse access string: %w", err)
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
	r.waitForUserActiveState(userId)

	if err = r.associateUserWithReplicationGroup(userId); err != nil {
		r.logger.Debug("encountered error while associating newly created user, attempting to clean up", "user id", userId)
		_, _ = r.DeleteUser(nil, dbplugin.DeleteUserRequest{
			Username: userId,
		})
		return dbplugin.NewUserResponse{}, fmt.Errorf("unable to configure newly created user %s: %w", userId, err)
	}

	return dbplugin.NewUserResponse{Username: *output.UserName}, nil
}

func (r *redisElastiCacheDB) UpdateUser(_ context.Context, req dbplugin.UpdateUserRequest) (dbplugin.UpdateUserResponse, error) {
	r.logger.Debug("updating AWS ElastiCache Redis user", "username", req.Username)

	userId := normaliseId(req.Username)

	if r.waitForUserActiveState(userId) {
		_, err := r.client.ModifyUser(&elasticache.ModifyUserInput{
			UserId:    &userId,
			Passwords: []*string{&req.Password.NewPassword},
		})
		if err != nil {
			return dbplugin.UpdateUserResponse{}, fmt.Errorf("unable to update user %s: %w", userId, err)
		}

		return dbplugin.UpdateUserResponse{}, nil
	} else {
		return dbplugin.UpdateUserResponse{}, fmt.Errorf("unable to update user %s, user never turned active", userId)
	}
}

func (r *redisElastiCacheDB) DeleteUser(_ context.Context, req dbplugin.DeleteUserRequest) (dbplugin.DeleteUserResponse, error) {
	r.logger.Debug("deleting AWS ElastiCache Redis user", "username", req.Username)

	userId := normaliseId(req.Username)

	out, err := r.client.DescribeUsers(&elasticache.DescribeUsersInput{UserId: aws.String(userId)})
	if (err != nil && err.(awserr.Error).Code() == "UserNotFound") || (out != nil && len(out.Users) == 1 && *out.Users[0].Status == "deleting") {
		r.logger.Debug("user does not exist or is being deleted, considering deletion successful", "user id", userId)
		return dbplugin.DeleteUserResponse{}, nil
	}

	if r.waitForUserActiveState(userId) {
		_, err = r.client.DeleteUser(&elasticache.DeleteUserInput{
			UserId: &userId,
		})
	} else {
		err = fmt.Errorf("unable to delete user %s, user never turned active", userId)
	}

	return dbplugin.DeleteUserResponse{}, err
}

// If replication group already has an associated user group, we use it
// If not and a default user group already exists, we associate it
// If not and a default group does not exist, we create and associate it
func (r *redisElastiCacheDB) associateUserWithReplicationGroup(userId string) error {
	replicationGroupId := r.extractReplicationGroupId()
	userGroupId, err := r.extractUserGroupId(replicationGroupId)
	if err != nil {
		return fmt.Errorf("unable to determine if replication group %s already has an associated user group", replicationGroupId)
	}

	if userGroupId != "" {
		if r.waitForGroupActiveState(userGroupId) {
			_, err = r.client.ModifyUserGroup(&elasticache.ModifyUserGroupInput{
				UserGroupId:  aws.String(userGroupId),
				UserIdsToAdd: []*string{aws.String(userId)},
			})
		} else {
			err = fmt.Errorf("unable to update user group %s, user group never turned active", replicationGroupId)
		}
	} else {
		r.logger.Debug("configuring user group for replication group", "replication group id", replicationGroupId)

		_, err = r.client.DescribeUserGroups(&elasticache.DescribeUserGroupsInput{
			UserGroupId: aws.String(replicationGroupId),
		})
		if err != nil && err.(awserr.Error).Code() == "UserGroupNotFound" {
			_, err = r.client.CreateUserGroup(&elasticache.CreateUserGroupInput{
				Engine:      aws.String("Redis"),
				Tags:        []*elasticache.Tag{},
				UserGroupId: aws.String(replicationGroupId),
				UserIds: []*string{
					aws.String("default"), // User groups must contain a user with the username default
					aws.String(userId),
				},
			})
		}

		if r.waitForGroupActiveState(replicationGroupId) {
			_, err = r.client.ModifyReplicationGroup(&elasticache.ModifyReplicationGroupInput{
				ReplicationGroupId: aws.String(replicationGroupId),
				UserGroupIdsToAdd:  []*string{aws.String(replicationGroupId)},
			})
		} else {
			err = fmt.Errorf("unable to update replication group %s, newly created user group never turned active", replicationGroupId)
		}
	}

	return err
}

// Replication groups can have none or up to 1 associated user group at any given time
func (r *redisElastiCacheDB) extractUserGroupId(replicationGroupId string) (string, error) {
	out, err := r.client.DescribeReplicationGroups(&elasticache.DescribeReplicationGroupsInput{
		ReplicationGroupId: aws.String(replicationGroupId),
	})

	userGroupId := ""
	if err == nil && len(out.ReplicationGroups) == 1 && len(out.ReplicationGroups[0].UserGroupIds) == 1 {
		userGroupId = *out.ReplicationGroups[0].UserGroupIds[0]
	}

	return userGroupId, err
}

// Elasticache URLs are always in the form of prefix.cluster-id.dns-suffix:port and cluster ids nor prefix cannot contain "." characters
func (r *redisElastiCacheDB) extractReplicationGroupId() string {
	_, after, found := strings.Cut(r.config.Url, ".")
	if found {
		before, _, found := strings.Cut(after, ".")
		if found {
			return normaliseId(before)
		}
	}

	return ""
}

func (r *redisElastiCacheDB) waitForGroupActiveState(userGroupId string) bool {
	return retry(userGroupId, func(s string) (bool, error) {
		out, err := r.client.DescribeUserGroups(&elasticache.DescribeUserGroupsInput{
			UserGroupId: aws.String(userGroupId),
		})

		if err != nil {
			return false, err
		} else {
			return len(out.UserGroups) == 1 && *out.UserGroups[0].Status == "active", nil
		}
	})
}

func (r *redisElastiCacheDB) waitForUserActiveState(userId string) bool {
	return retry(userId, func(s string) (bool, error) {
		out, err := r.client.DescribeUsers(&elasticache.DescribeUsersInput{
			UserId: aws.String(userId),
		})

		if err != nil {
			return false, err
		} else {
			return len(out.Users) == 1 && *out.Users[0].Status == "active", nil
		}
	})
}

func retry(s string, f func(string) (bool, error)) bool {
	for i := 0; i < 50; i++ {
		ok, err := f(s)
		if err != nil {
			return false
		} else if ok {
			return true
		} else {
			time.Sleep(3 * time.Second)
		}
	}

	return false
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

		if len(rules) > 0 {
			accessString += strings.Join(rules, " ")
			accessString += " "
		}
	}

	if strings.HasPrefix(accessString, "off ") || strings.Contains(accessString, " off ") || strings.HasSuffix(accessString, " off") {
		return "", errors.New("creation of disabled or 'off' users is forbidden")
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
