# Vault Plugin Scaffolding

This is a standalone backend plugin for use with [Hashicorp
Vault](https://www.github.com/hashicorp/vault).

[//]: <> (Include a general statement about this plugin)

Please note: We take Vault's security and our users' trust very seriously. If
you believe you have found a security issue in Vault, please responsibly
disclose by contacting us at [security@hashicorp.com](mailto:security@hashicorp.com).

## Using this Template Repository

_Note: Remove this instruction sub-heading once you've created a repository from this template_

This repository is a template for a Vault secret engine and auth method plugins.
It is intended as a starting point for creating Vault plugins, containing:

- Changelog, readme, Makefile, pull request template
- Scripts for internal tooling
- Jira sync and basic testing GitHub actions
- A base `main.go` for compiling the plugin

There's some minimal GitHub Secrets setup required in order to get the Jira sync
GH action working. Install the `gh` [CLI](https://cli.github.com/manual/) and
perform the following commands to set secrets for this repository.

```sh
gh secret set JIRA_SYNC_BASE_URL 
gh secret set JIRA_SYNC_USER_EMAIL 
gh secret set JIRA_SYNC_API_TOKEN
```


This template repository does not include a Mozilla Public License 2.0 `LICENSE`
since plugins created this way can be internal to hashicorp and for Vault
Enterprise consumption. To add a license, follow [these GitHub
instructions](https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions/adding-a-license-to-a-repository),
or obtain one from one of our public Vault plugins.

Please see the [GitHub template repository
documentation](https://help.github.com/en/github/creating-cloning-and-archiving-repositories/creating-a-repository-from-a-template)
for how to create a new repository from this template on GitHub.

Things _not_ handled by this template repository:
- Repository settings, such as branch protection rules
- Memberships and permissions
- GitHub secrets for this repository

Please see the [Repository Configuration Page](https://hashicorp.atlassian.net/wiki/spaces/VAULT/pages/2103476333/Repository+Configuration)
for the setting proper repository configuration values.

## Quick Links

- [Vault Website](https://www.vaultproject.io)
- [Vault Project GitHub](https://www.github.com/hashicorp/vault)

[//]: <> (Include any other quick links relevant to your plugin)

## Getting Started

This is a [Vault plugin](https://www.vaultproject.io/docs/plugins)
and is meant to work with Vault. This guide assumes you have already installed
Vault and have a basic understanding of how Vault works.

Otherwise, first read this guide on how to [get started with
Vault](https://www.vaultproject.io/intro/getting-started/install.html).


## Usage

[//]: <> (Provide usage instructions and/or links to this plugin)

## Developing

If you wish to work on this plugin, you'll first need
[Go](https://www.golang.org) installed on your machine.

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
$ SHA256=$(openssl dgst -sha256 $GOPATH/vault-plugin-secrets-myplugin | cut -d ' ' -f2)
$ vault plugin register \
        -sha256=$SHA256 \
        -command="vault-plugin-secrets-myplugin" \
        secrets myplugin
...
Success! Data written to: sys/plugins/catalog/myplugin
```

Enable the secrets engine to use this plugin:

```sh
$ vault secrets enable myplugin
...

Successfully enabled 'plugin' at 'myplugin'!
```

### Tests

To run the tests, invoke `make test`:

```sh
$ make test
```

You can also specify a `TESTARGS` variable to filter tests like so:

```sh
$ make test TESTARGS='-run=TestConfig'
```

[//]: <> (Specify any other test instructions such as acceptance/integration tests)
