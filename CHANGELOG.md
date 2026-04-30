# Changelog

## [0.19.4](https://github.com/Apollogeddon/distribyted/compare/v0.19.3...v0.19.4) (2026-04-30)


### Bug Fixes

* Enhance logging for filesystem operations ([835f852](https://github.com/Apollogeddon/distribyted/commit/835f852911cff12fbe13929cbc89173ab2ed7be6))
* Implement inode generation and hashing for torrent files ([49a7557](https://github.com/Apollogeddon/distribyted/commit/49a75577a268af5f00de2d432f251022c39ec38e))
* Restrict CodeQL analysis to Go language only ([f069e7b](https://github.com/Apollogeddon/distribyted/commit/f069e7b7002aaadbf014c3297b3c3d34c6963b5e))

## [0.19.3](https://github.com/Apollogeddon/distribyted/compare/v0.19.2...v0.19.3) (2026-04-30)


### Bug Fixes

* Implement link management and enhance filesystem operations ([487dd6f](https://github.com/Apollogeddon/distribyted/commit/487dd6f66884acdaf5dddddc4eeaaaa71366d983))

## [0.19.2](https://github.com/Apollogeddon/distribyted/compare/v0.19.1...v0.19.2) (2026-04-30)


### Bug Fixes

* Enhance filesystem stats and logging for torrent removal ([588f7b4](https://github.com/Apollogeddon/distribyted/commit/588f7b4fb5340bf6c6ceb994f2e6042ebdf52d5b))

## [0.19.1](https://github.com/Apollogeddon/distribyted/compare/v0.19.0...v0.19.1) (2026-04-29)


### Bug Fixes

* Implement Create and Remove methods for Filesystem interface ([1cfb241](https://github.com/Apollogeddon/distribyted/commit/1cfb241c1c27664e2737277ed22b0f7e072d951c))

## [0.19.0](https://github.com/Apollogeddon/distribyted/compare/v0.18.3...v0.19.0) (2026-04-29)


### Features

* Add mock implementations for file operations in fuse mount ([bfed397](https://github.com/Apollogeddon/distribyted/commit/bfed39725ac7a51e476ad27619b7c04555d2e243))


### Bug Fixes

* Update fuse path handling in torrent categories and preferences ([4452fb7](https://github.com/Apollogeddon/distribyted/commit/4452fb7cf16e015526b9ddf6762e2ad05a76a157))

## [0.18.3](https://github.com/Apollogeddon/distribyted/compare/v0.18.2...v0.18.3) (2026-04-29)


### Bug Fixes

* Initialize storage with root directory on creation ([eea36ac](https://github.com/Apollogeddon/distribyted/commit/eea36ac9546220fe5971cd62028d61dc844335d6))

## [0.18.2](https://github.com/Apollogeddon/distribyted/compare/v0.18.1...v0.18.2) (2026-04-29)


### Bug Fixes

* Continuing to try and resolve the issues with the roues waiting for torrents to exist ([d1c7711](https://github.com/Apollogeddon/distribyted/commit/d1c7711ad1a7c1b439555b240a0e017e32be6754))
* Streamline torrent file addition and improve route handling ([0a6836b](https://github.com/Apollogeddon/distribyted/commit/0a6836bba50c582eef7be33a158bb946c98db53e))

## [0.18.1](https://github.com/Apollogeddon/distribyted/compare/v0.18.0...v0.18.1) (2026-04-29)


### Bug Fixes

* Handle error when writing to temporary log file in TestApiLogHandler ([30ff2da](https://github.com/Apollogeddon/distribyted/commit/30ff2da22c562cc50377f0c6f2de6d79a65fe444))
* Implement torrentService interface and add unit tests for API handlers ([b0c796b](https://github.com/Apollogeddon/distribyted/commit/b0c796b46b0d3404464d8502e3431a21a7f700e2))
* Update torrent state from 'seeding' to 'uploading' and add route in addTorrent method ([86ef63e](https://github.com/Apollogeddon/distribyted/commit/86ef63e516480424ad39e3e1c1f15c8e528051fd))

## [0.18.0](https://github.com/Apollogeddon/distribyted/compare/v0.17.2...v0.18.0) (2026-04-29)


### Features

* Update config to by default allow other users access and add new test cases for handlers and webdav ([4c0c6cb](https://github.com/Apollogeddon/distribyted/commit/4c0c6cb58ece26e8ff9a253e6430c790de5c1516))

## [0.17.2](https://github.com/Apollogeddon/distribyted/compare/v0.17.1...v0.17.2) (2026-04-29)


### Bug Fixes

* Handle root directory case in getDirFromFs to return os.ErrNotExist ([c59d158](https://github.com/Apollogeddon/distribyted/commit/c59d15869bf31cf446c7644b4175417b028e3fba))
* Update qBitTorrent category handlers to include config handler and create category functionality ([52f4230](https://github.com/Apollogeddon/distribyted/commit/52f42301901326d96eb474690bd4ff677e2504b5))

## [0.17.1](https://github.com/Apollogeddon/distribyted/compare/v0.17.0...v0.17.1) (2026-04-29)


### Bug Fixes

* Ensure root directory is added during ContainerFs initialization ([a657e7a](https://github.com/Apollogeddon/distribyted/commit/a657e7af6e1d1221c4f3f4ef4851c8ce317dc63c))

## [0.17.0](https://github.com/Apollogeddon/distribyted/compare/v0.16.2...v0.17.0) (2026-04-29)


### Features

* Implement Mkdir and Rmdir methods for Filesystem interface and related types ([6673a47](https://github.com/Apollogeddon/distribyted/commit/6673a470efbe8798e181f4375a5eea057636c1d8))


### Bug Fixes

* Add Mkdir and Rmdir methods to DummyFs and update WebDAV tests ([cfa5426](https://github.com/Apollogeddon/distribyted/commit/cfa5426656ea7f7b0fb9a8fe9a3b72f09daf4a82))
* Add qBittorrent API endpoint for creating categories ([95ea9c1](https://github.com/Apollogeddon/distribyted/commit/95ea9c15716c74b29014e1f98cebd6f4d3a32ffc))
* Create a generic mock handler for extra api calls ([954831c](https://github.com/Apollogeddon/distribyted/commit/954831cc18a98bfe4b81f2eb7d23ed1ea9373a51))
* Update job quality control job to run in parallel ([5abcd3b](https://github.com/Apollogeddon/distribyted/commit/5abcd3bab4b64c8e4faf52dc7db358def7538d54))

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
