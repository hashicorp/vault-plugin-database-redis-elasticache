provider "aws" {
  // Credentials and configuration derived from the environment
  // Uncomment if you wish to configure the provider explicitly

  // access_key = ""
  // secret_key = ""
  // region = ""
}

resource "aws_elasticache_cluster" "vault_plugin_elasticache_test" {
  cluster_id           = "vault-plugin-elasticache-test"
  engine               = "redis"
  engine_version       = "6.2"
  node_type            = "cache.t4g.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis6.x"

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
    resources = ["arn:aws:elasticache:*:*:user:*"]
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
  value = aws_iam_access_key.vault_plugin_elasticache_test.secret
}

// export TEST_ELASTICACHE_URL=${url}
output "url" {
  value = format(
    "%s:%s",
    aws_elasticache_cluster.vault_plugin_elasticache_test.cache_nodes[0].address,
    aws_elasticache_cluster.vault_plugin_elasticache_test.port)
}

// export TEST_ELASTICACHE_REGION=${region}
data "aws_region" "current" {}
output "region" {
  value = data.aws_region.current.name
}
