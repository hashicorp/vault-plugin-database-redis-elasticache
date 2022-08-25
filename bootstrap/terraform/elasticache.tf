provider "aws" {
  // Credentials and configuration derived from the environment
  // Uncomment if you wish to configure the provider explicitly

  // access_key = ""
  // secret_key = ""
  // region = ""
}

resource "aws_elasticache_replication_group" "vault_plugin_elasticache_test" {
  replication_group_id       = "vault-plugin-elasticache-test"
  description                = "vault elasticache plugin generated test cluster"
  engine                     = "redis"
  engine_version             = "6.2"
  node_type                  = "cache.t4g.micro"
  num_cache_clusters         = 1
  parameter_group_name       = "default.redis6.x"
  transit_encryption_enabled = true

  tags = {
    "description" : "vault elasticache plugin generated test cluster"
  }
}

resource "aws_iam_user" "vault_plugin_elasticache_test" {
  name = "vault-plugin-elasticache-user-test"

  tags = {
    "description" : "vault elasticache plugin generated test user"
  }
}

resource "aws_iam_access_key" "vault_plugin_elasticache_test" {
  user = aws_iam_user.vault_plugin_elasticache_test.name
}

resource "aws_iam_user_policy" "vault_plugin_elasticache_test" {
  name = "vault-plugin-elasticache-policy-test"
  user = aws_iam_user.vault_plugin_elasticache_test.name

  policy = data.aws_iam_policy_document.vault_plugin_elasticache_test.json
}

data "aws_iam_policy_document" "vault_plugin_elasticache_test" {
  statement {
    actions = [
      "elasticache:DescribeUsers",
      "elasticache:CreateUser",
      "elasticache:ModifyUser",
      "elasticache:DeleteUser",
    ]
    resources = [
      "arn:aws:elasticache:*:*:user:*",
    ]
  }

  statement {
    actions = [
      "elasticache:DescribeUserGroups",
      "elasticache:CreateUserGroup",
      "elasticache:ModifyUserGroup",
      "elasticache:DeleteUserGroup",
      "elasticache:ModifyReplicationGroup",
    ]
    resources = [
      "arn:aws:elasticache:*:*:usergroup:*",
    ]
  }

  statement {
    actions = [
      "elasticache:DescribeReplicationGroups",
      "elasticache:ModifyReplicationGroup",
    ]
    resources = [
      "arn:aws:elasticache:*:*:replicationgroup:*",
    ]
  }
}

// export TEST_ELASTICACHE_USERNAME=${username}
output "username" {
  value = aws_iam_access_key.vault_plugin_elasticache_test.id
}

// export TEST_ELASTICACHE_PASSWORD=${password}
// Use `terraform output password` to access the value
output "password" {
  sensitive = true
  value     = aws_iam_access_key.vault_plugin_elasticache_test.secret
}

// export TEST_ELASTICACHE_URL=${url}
output "url" {
  value = format(
    "%s:%s",
    aws_elasticache_replication_group.vault_plugin_elasticache_test.primary_endpoint_address,
  aws_elasticache_replication_group.vault_plugin_elasticache_test.port)
}

// export TEST_ELASTICACHE_REGION=${region}
data "aws_region" "current" {}
output "region" {
  value = data.aws_region.current.name
}
