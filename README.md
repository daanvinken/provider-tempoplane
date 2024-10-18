# Provider tempoplane
![Logo](logo.png)


Provider for Crossplane to manage and orchestrate Temporal-based workflows using Tempoplane.

## Features
- Manage workflows as Crossplane custom resources.
- Observe, create, update, and delete workflows from Kubernetes.

## Usage
1. Install Crossplane.
2. Deploy the `provider-tempoplane`.
3. Define `EntityWorkflow` custom resources to manage your workflows.
4. Configure your worker via https://github.com/daanvinken/tempoplane

## Developing

1. Use this repository as a tempoplane to create a new one.
1. Run `make submodules` to initialize the "build" Make submodule we use for CI/CD.
1. Rename the provider by running the following command:
```shell
  export provider_name=MyProvider # Camel case, e.g. GitHub
  make provider.prepare provider=${provider_name}
```
4. Add your new type by running the following command:
```shell
  export group=sample # lower case e.g. core, cache, database, storage, etc.
  export type=MyType # Camel casee.g. Bucket, Database, CacheCluster, etc.
  make provider.addtype provider=${provider_name} group=${group} kind=${type}
```
5. Replace the *sample* group with your new group in apis/{provider}.go
5. Replace the *mytype* type with your new type in internal/controller/{provider}.go
5. Replace the default controller and ProviderConfig implementations with your own
5. Run `make reviewable` to run code generation, linters, and tests.
5. Run `make build` to build the provider.

Refer to Crossplane's [CONTRIBUTING.md] file for more information on how the
Crossplane community prefers to work. The [Provider Development][provider-dev]
guide may also be of use.

[CONTRIBUTING.md]: https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md
[provider-dev]: https://github.com/crossplane/crossplane/blob/master/contributing/guide-provider-development.md
