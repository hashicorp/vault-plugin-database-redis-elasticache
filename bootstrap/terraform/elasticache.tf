# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

provider "aws" {
  // Credentials and configuration derived from the environment
  // Uncomment if you wish to configure the provider explicitly

  // access_key = ""
  // secret_key = ""
  // region = ""
}

resource "random_password" "vault_plugin_elasticache_test" {
  length = 16
}

// Using a randomly generated suffix so there are no collisions if multiple CI workflows run in parallel
resource "random_string" "suffix" {
  length           = 8
  special          = false
}

# resource "aws_elasticache_replication_group" "vault_plugin_elasticache_test" {
#   replication_group_id       = "vault-plugin-elasticache-test-${random_string.suffix.result}"
#   description                = "vault elasticache plugin generated test cluster"
#   engine                     = "redis"
#   engine_version             = "6.2"
#   node_type                  = "cache.t4g.micro"
#   num_cache_clusters         = 1
#   parameter_group_name       = "default.redis6.x"
#   transit_encryption_enabled = true
#   user_group_ids             = [aws_elasticache_user_group.vault_plugin_elasticache_test.id]
#
#   tags = {
#     "description" : "vault elasticache plugin generated test cluster"
#   }
# }
#
# resource "aws_elasticache_user_group" "vault_plugin_elasticache_test" {
#   engine        = "REDIS"
#   user_group_id = "vault-test-user-group-${random_string.suffix.result}"
#   user_ids      = ["default", aws_elasticache_user.vault_plugin_elasticache_test.user_id]
# }
#
# resource "aws_elasticache_user" "vault_plugin_elasticache_test" {
#   user_id       = "vault-test-${random_string.suffix.result}"
#   user_name     = "vault-test-${random_string.suffix.result}"
#   access_string = "on ~* +@all"
#   engine        = "REDIS"
#   passwords     = [random_password.vault_plugin_elasticache_test.result]
# }

resource "aws_iam_user" "vault_plugin_elasticache_test" {
  name = "vault-plugin-elasticache-user-test-${random_string.suffix.result}"

  tags = {
    "description" : "vault elasticache plugin generated test user"
  }
}

resource "aws_iam_access_key" "vault_plugin_elasticache_test" {
  user = aws_iam_user.vault_plugin_elasticache_test.name
}

resource "aws_iam_user_policy" "vault_plugin_elasticache_test" {
  name = "vault-plugin-elasticache-policy-test-${random_string.suffix.result}"
  user = aws_iam_user.vault_plugin_elasticache_test.name

  policy = data.aws_iam_policy_document.vault_plugin_elasticache_test.json
}

data "aws_iam_policy_document" "vault_plugin_elasticache_test" {
  statement {
    actions = [
      "elasticache:DescribeUsers",
      "elasticache:ModifyUser",
    ]
    resources = [
      "arn:aws:elasticache:*:*:user:*",
    ]
  }
}

data "aws_region" "current" {}
resource "local_file" "setup_environment_file" {
  filename = "local_environment_setup.sh"
  content = <<EOF
export TEST_ELASTICACHE_ACCESS_KEY_ID=${aws_iam_access_key.vault_plugin_elasticache_test.id} &&\
export TEST_ELASTICACHE_SECRET_ACCESS_KEY=${aws_iam_access_key.vault_plugin_elasticache_test.secret} &&\
export TEST_ELASTICACHE_URL=master.vault-plugin-elasticache-test-jhgljn2l.6ylpiw.use1.cache.amazonaws.com:6379 &&\
export TEST_ELASTICACHE_REGION=${data.aws_region.current.name} &&\
export TEST_ELASTICACHE_USER=vault-test-jhGljN2l
EOF
}