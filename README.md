![Baton Logo](./baton-logo.png)

# `baton-eks` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-eks.svg)](https://pkg.go.dev/github.com/conductorone/baton-eks) ![main ci](https://github.com/conductorone/baton-eks/actions/workflows/main.yaml/badge.svg)

`baton-eks` is a connector for built using the [Baton SDK](https://github.com/conductorone/baton-sdk).

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Prerequisites
No prerequisites were specified for `baton-eks`

# Getting Started

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-eks
baton-eks
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_DOMAIN_URL=domain_url -e BATON_API_KEY=apiKey -e BATON_USERNAME=username ghcr.io/conductorone/baton-eks:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-eks/cmd/baton-eks@main

baton-eks

baton resources
```

# Data Model

`baton-eks` will pull down information about the following resources:
- Users
- Groups
- Roles
- Cluster roles
- Namespaces

`baton-eks` does not specify supporting account provisioning or entitlement provisioning.

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually
building spreadsheets. We welcome contributions, and ideas, no matter how
small&mdash;our goal is to make identity and permissions sprawl less painful for
everyone. If you have questions, problems, or ideas: Please open a GitHub Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-eks` Command Line Usage

```
baton-eks

Usage:
  baton-eks [flags]
  baton-eks [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --client-id string             The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string         The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
      --eks-acces-key string         The access key of the user ($BATON_EKS_ACCESS_KEY)
      --eks-secret-access-key string The secret access key of the user ($BATON_EKS_SECRET_ACCESS_KEY)
      --eks-region string            The AWS account region ($BATON_EKS_REGION)
      --eks-assume-role-arn string   The arn of the IAM role to assume ($BATON_EKS_ASSUME_ROLE_ARN)
      --eks-cluster-name string      The name of the cluster to sync ($BATON_EKS_CLUSTER_NAME)
  -f, --file string                  The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                         help for baton-eks
      --log-format string            The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string             The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
  -p, --provisioning                 If this connector supports provisioning, this must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --ticketing                    This must be set to enable ticketing support ($BATON_TICKETING)
  -v, --version                      version for baton-eks

Use "baton-eks [command] --help" for more information about a command.
```
