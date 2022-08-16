# Vault Plugin Database Redis ElastiCache

This is a standalone [Database Plugin](https://www.vaultproject.io/docs/secrets/databases) for use with [Hashicorp
Vault](https://www.github.com/hashicorp/vault).

This plugin supports exclusively AWS ElastiCache for Redis. [Redis Enterprise](https://github.com/RedisLabs/vault-plugin-database-redis-enterprise) 
and [Redis Open Source](https://github.com/fhitchen/vault-plugin-database-redis) use different plugins.

Please note: We take Vault's security and our users' trust very seriously. If
you believe you have found a security issue in Vault, please responsibly
disclose by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).


## Quick Links

- [Vault Website](https://www.vaultproject.io)
- [Plugin System](https://www.vaultproject.io/docs/plugins)


## Getting Started

This is a [Vault plugin](https://www.vaultproject.io/docs/plugins)
and is meant to work with Vault. This guide assumes you have already installed
Vault and have a basic understanding of how Vault works.

Otherwise, first read this guide on how to [get started with
Vault](https://www.vaultproject.io/intro/getting-started/install.html).


## Development

If you wish to work on this plugin, you'll first need
[Go](https://www.golang.org) installed on your machine (version 1.17+ recommended)

Make sure Go is properly installed, including setting up a [GOPATH](https://golang.org/doc/code.html#GOPATH).

To run the tests locally you will need to have write permissions to an [ElastiCache for Redis](https://aws.amazon.com/elasticache/redis/) instance.

## Building

If you're developing for the first time, run `make bootstrap` to install the
necessary tools. Bootstrap will also update repository name references if that
has not been performed ever before.

```sh
$ make bootstrap
```

To compile a development version of this plugin, run `make` or `make dev`.
This will put the plugin binary in the `bin` and `$GOPATH/bin` folders. `dev`
mode will only generate the binary for your platform and is faster:

```sh
$ make dev
```

## Tests

### Testing Manually

Put the plugin binary into a location of your choice. This directory
will be specified as the [`plugin_directory`](https://www.vaultproject.io/docs/configuration#plugin_directory)
in the Vault config used to start the server.

```hcl
# config.hcl
plugin_directory = "path/to/plugin/directory"
...
```

Start a Vault server with this config file:

```sh
$ vault server -dev -config=path/to/config.hcl ...
...
```

Once the server is started, register the plugin in the Vault server's [plugin catalog](https://www.vaultproject.io/docs/plugins/plugin-architecture#plugin-catalog):

```sh
$ SHA256=$(openssl dgst -sha256 $GOPATH/vault-plugin-database-redis-elasticache | cut -d ' ' -f2)
$ vault write sys/plugins/catalog/database/vault-plugin-database-redis-elasticache \
        command=vault-plugin-database-redis-elasticache \
        sha256=$SHA256
...
Success! Data written to: sys/plugins/catalog/database/vault-plugin-database-redis-elasticache
```

Enable the database engine to use this plugin:

```sh
$ vault secrets enable database
...

Success! Enabled the database secrets engine at: database/
```

Once the database engine is enabled you can configure an ElastiCache instance:

```sh
$ vault write database/config/redis-mydb \
        plugin_name="vault-plugin-database-redis-elasticache" \
        username=$USERNAME \
        password=$PASSWORD \
        url=$URL \
        region=$REGION
...

Success! Data written to: database/config/redis-mydb
```

Configure a role:

```sh
$ vault write database/roles/redis-myrole \
        db_name="redis-mydb" \
        creation_statements=$CREATION_STATEMENTS \
        default_ttl=$DEFAULT_TTL \
        max_ttl=$MAX_TTL
...

Success! Data written to: database/roles/redis-myrole
```

And generate your first set of dynamic credentials:

```sh
$ vault read database/creds/redis-myrole
...

Key                Value
---                -----
lease_id           database/creds/redis-myrole/ID
lease_duration     Xm
lease_renewable    true
password           PASSWORD
username           v_token_redis-myrole_ID_EPOCH
```


### Automated Tests

To run the tests, invoke `make test`:

```sh
$ make test
```

You can also specify a `TESTARGS` variable to filter tests like so:

```sh
$ make test TESTARGS='-run=TestConfig'
```
