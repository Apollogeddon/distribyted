# Changelog

## [0.20.1](https://github.com/Apollogeddon/distribyted/compare/v0.20.0...v0.20.1) (2026-05-03)


### Bug Fixes

* Add missing runtime dependencies for FUSE support ([a70a71d](https://github.com/Apollogeddon/distribyted/commit/a70a71dc10bfc01b2661dfbd453c2caebef246fb))
* Increase buffer size in TestIntegration_P2PStall to trigger network request stall ([7ccb5aa](https://github.com/Apollogeddon/distribyted/commit/7ccb5aa3b3d70743a9101f1f10c69f078434f82e))
* Reduce read timeout for integration tests and restore commented-out persistence test ([c49aba8](https://github.com/Apollogeddon/distribyted/commit/c49aba8292a02dff6a657965e8926f3e0610811c))
* Streamline file removal process and enhance read wrapper functionality ([ac65276](https://github.com/Apollogeddon/distribyted/commit/ac65276e2b5f1a88e6ba9cb0d0c20460fb1aceb5))

## [0.20.0](https://github.com/Apollogeddon/distribyted/compare/v0.19.4...v0.20.0) (2026-05-03)


### Features

* Add DHT, UPnP, and ListenPort configuration options ([0ab8798](https://github.com/Apollogeddon/distribyted/commit/0ab879842a27bd74e157adc714d46eb5401f4da7))
* Expand qBitTorrent API v2 compatibility ([66646e1](https://github.com/Apollogeddon/distribyted/commit/66646e1264dd4b0cffff38ea8cda0348b188623e))
* Implement configurable watcher interval and fix VFS modtime TODOs ([621ed3d](https://github.com/Apollogeddon/distribyted/commit/621ed3d25f9519b4ecf1e7b26bb6e69e3144e4f3))
* Modularize WebDAV server and handler ([16c92ea](https://github.com/Apollogeddon/distribyted/commit/16c92ea43633ef8722e08479fa1168f76c7cf969))


### Bug Fixes

* Add force unmount helper for Unix systems ([3a1e36b](https://github.com/Apollogeddon/distribyted/commit/3a1e36b35035b66a7c609f388bac969ecbdedf44))
* Add mutex locking to MemoryFile and mockHost for thread safety ([a94894e](https://github.com/Apollogeddon/distribyted/commit/a94894eda70d930ffc2b9b64a117ee21fb402ab4))
* Add mutex locking to MockLoaderAdder for thread safety ([df72fc7](https://github.com/Apollogeddon/distribyted/commit/df72fc750583887551a20f9a54be0a9a694f9f46))
* Add mutex locking to storage methods for thread safety ([93ebd65](https://github.com/Apollogeddon/distribyted/commit/93ebd65d6e8aa70cdcf7e35c8eb8666fc469809b))
* Change read lock to write lock in Server Info method for thread safety ([0fb6c6d](https://github.com/Apollogeddon/distribyted/commit/0fb6c6d75a723267ab779b79f1d5c66ed4d01208))
* Correct MemoryFile size calculation and improve struct formatting ([2daafe9](https://github.com/Apollogeddon/distribyted/commit/2daafe996777dd451e8401b55c44b7c44cbe26e5))
* Enable continue on add timeout and improve logging for torrent addition ([ba9268f](https://github.com/Apollogeddon/distribyted/commit/ba9268f9eae6e2152681e3be20b9ea7826b03f85))
* Ensure proper closure of resources with error handling in tests and handlers ([38ac3e2](https://github.com/Apollogeddon/distribyted/commit/38ac3e2b67151bdf9b619d6ffa3b70f9a3ab6737))
* Handle errors during temporary directory cleanup in tests ([e1aa08a](https://github.com/Apollogeddon/distribyted/commit/e1aa08abed71903db22a5eb8c441ff8b870db969))
* Handle errors on resource cleanup in tests ([fc7b3fb](https://github.com/Apollogeddon/distribyted/commit/fc7b3fba04c9fc9101bf15d49fda7710d4075683))
* Implement background retry logic for VFS virtual links ([4f28d83](https://github.com/Apollogeddon/distribyted/commit/4f28d8358da1ef68faa0d4a02acdf6c61cbdc375))
* Implement periodic BadgerDB garbage collection ([1858207](https://github.com/Apollogeddon/distribyted/commit/18582072891ad9293cfb0c78459f087818191795))
* Improve cleanup process by adding warning for failed torrent cache removal ([50ce152](https://github.com/Apollogeddon/distribyted/commit/50ce15232f16640a0324cfe4f54c6b08ffd80006))
* Improve logging safety and test environment reliability ([e3b7d59](https://github.com/Apollogeddon/distribyted/commit/e3b7d59e5433d0040c54712e4c76b6d9776a17a1))
* Improve mount stability and error handling ([b357cbc](https://github.com/Apollogeddon/distribyted/commit/b357cbc836789a8e173c0b35d93d3f704b649c11))
* Improve resource cleanup in tests by handling errors on closure ([69c740e](https://github.com/Apollogeddon/distribyted/commit/69c740e6b37db682019742098a7031373bd09290))
* Refactor magnet retrieval in LiveServerUpdates test for clarity and efficiency ([c480bb2](https://github.com/Apollogeddon/distribyted/commit/c480bb241197b0d09b9845798274509e9f8eb7ae))
* Remove undefined GOARM variable from build workflow ([98a1262](https://github.com/Apollogeddon/distribyted/commit/98a12628fafffefe7f0c870a4e29fdff71c66353))
* Resolve data races and optimize database metadata storage ([4adcc6e](https://github.com/Apollogeddon/distribyted/commit/4adcc6ef82a3acfa06d4019f4a6e6f073dc86ada))
* Suppress error on temporary directory removal in seeder ([f8a8385](https://github.com/Apollogeddon/distribyted/commit/f8a8385a429163b89a1f5bc0023f3840819565c5))
* Update Info method to return ServerInfo by value and refactor magnet retrieval in integration tests ([f330361](https://github.com/Apollogeddon/distribyted/commit/f330361eec940c3c4307dc01af53c343873df03b))
* Update timeout settings in TestApp configuration for improved performance ([30d614e](https://github.com/Apollogeddon/distribyted/commit/30d614e60c4af39255d99b4b382f53238806ccc2))

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
