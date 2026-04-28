# Changelog

## [0.16.2](https://github.com/Apollogeddon/distribyted/compare/v0.16.1...v0.16.2) (2026-04-28)


### Bug Fixes

* Add qBittorrent API endpoints for preferences management ([c84b052](https://github.com/Apollogeddon/distribyted/commit/c84b052d2b762678876383c78e8cf5be7a44d0da))

## [0.16.1](https://github.com/Apollogeddon/distribyted/compare/v0.16.0...v0.16.1) (2026-04-28)


### Bug Fixes

* Enhance Docker workflow to accept version input and streamline conditional execution ([0e287b3](https://github.com/Apollogeddon/distribyted/commit/0e287b396d318961f7a2e4ec941d6663026a7b8b))
* Update pion/dtls and pion/transport dependencies to latest versions ([7830e82](https://github.com/Apollogeddon/distribyted/commit/7830e82cc11f254e255718f9a233cdd49e2cbbc8))

## [0.16.0](https://github.com/Apollogeddon/distribyted/compare/v0.15.0...v0.16.0) (2026-04-28)


### Features

* Add release automation with release-please and update main application versioning ([fd8f196](https://github.com/Apollogeddon/distribyted/commit/fd8f196e0a7d0b13968f40c9868b84828d004743))
* Avoid timeout errors on start. ([#272](https://github.com/Apollogeddon/distribyted/issues/272)) ([a8166eb](https://github.com/Apollogeddon/distribyted/commit/a8166eb3b48b097033f89345afd24dba252524c1))
* Implement Link and Rename methods for filesystem interfaces and a qbittorrent api ([d161f5d](https://github.com/Apollogeddon/distribyted/commit/d161f5d6bc4b78f3d5b363858ef18c870cacdfd4))
* Implement ListMagnets and ListTorrentPaths methods in Folder struct with tests ([de4e44f](https://github.com/Apollogeddon/distribyted/commit/de4e44fd53f4f1fd2159e02924084b442f4a1d3b))


### Bug Fixes

* Add install-mode to golangci-lint action configuration ([393e85b](https://github.com/Apollogeddon/distribyted/commit/393e85b423f700dc1baadd02fa49adfbda071c85))
* Add installation step for required libraries in golangci-lint workflow ([6d28c5f](https://github.com/Apollogeddon/distribyted/commit/6d28c5f93d6cf20a8167fa58e0c9cc04fe3c72d3))
* Add release configuration and update auto-merge conditions for Dependabot ([b24f32b](https://github.com/Apollogeddon/distribyted/commit/b24f32b6570ab18fe2afc875a3584aff2ed120aa))
* Don't clobber gopath except for the target that uses the var ([#11](https://github.com/Apollogeddon/distribyted/issues/11)) ([21f4a5f](https://github.com/Apollogeddon/distribyted/commit/21f4a5f1da9a75df05d1c90f00efee1ce0245256))
* Ensure 'all' target depends on 'build' in Makefile ([1dce2a0](https://github.com/Apollogeddon/distribyted/commit/1dce2a0b06634257aace1fd5ab9f6b416870fece))
* Pin golangci-lint version to v1.64.8 and remove Go version argument ([c3830f1](https://github.com/Apollogeddon/distribyted/commit/c3830f1ce1b64bf1b05fb8bd7c3c2dc2d0021266))
* Restricted golint to use 1.24 and fixed linting issues ([626a147](https://github.com/Apollogeddon/distribyted/commit/626a147cc2c090b07d02641703a7ba9ce42a4ba9))
* Update checkout actions to disable credential persistence ([734cc19](https://github.com/Apollogeddon/distribyted/commit/734cc19735262b80bc8c9968e4c7e9a3cf81d940))
* Update GitHub Action versions for Trivy and CodeQL uploads ([942e655](https://github.com/Apollogeddon/distribyted/commit/942e655580ab2c69cf285d2eb23821bc7b9c59a8))
* Update image reference for Trivy vulnerability scanner in Docker workflow ([48cba2c](https://github.com/Apollogeddon/distribyted/commit/48cba2cba8c7283076a5ab800301af286a8e1bef))
* Update image reference in Docker metadata action ([e574b9c](https://github.com/Apollogeddon/distribyted/commit/e574b9c152131b7199c91fd08ca4794e29568841))
* Update macOS version in build workflow and improve version retrieval in Makefile ([5ebd1a1](https://github.com/Apollogeddon/distribyted/commit/5ebd1a1e4cf1e65a4617ad4f3d19adabca47abf4))
* Update macOS version to 'macos-latest' and enable CGO for build step ([ef437d7](https://github.com/Apollogeddon/distribyted/commit/ef437d7e697671eac65f50e090cf773c39a3dbba))
* Update permissions for GitHub Actions and handle git fetch errors in MkDocs deployment ([8719069](https://github.com/Apollogeddon/distribyted/commit/8719069933f5f97b29e70e0376ead01dcbf8c6ef))
* Update workflows to install required libraries on Linux ([7e40864](https://github.com/Apollogeddon/distribyted/commit/7e40864cee0f0d10cc228ee8038247756b84e9b8))
