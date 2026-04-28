[![Releases][releases-shield]][releases-url]
[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![GPL3 License][license-shield]][license-url]
[![Coveralls][coveralls-shield]][coveralls-url]
[![Docker Image][docker-pulls-shield]][docker-pulls-url]
<!-- PROJECT LOGO -->
<br />
<p align="center">
  <a href="https://github.com/Apollogeddon/distribyted">
    <img src="mkdocs/docs/images/distribyted_icon.png" alt="Logo" width="100">
  </a>

  <h3 align="center">distribyted</h3>

  <p align="center">
    Torrent client with on-demand file downloading as a filesystem.
    <br />
    <br />
    <a href="https://github.com/Apollogeddon/distribyted/issues">Report a Bug</a>
    ·
    <a href="https://github.com/Apollogeddon/distribyted/issues">Request Feature</a>
  </p>
</p>

## About The Project

![Distribyted Screen Shot][product-screenshot]

Distribyted is an alternative torrent client that treats torrents as a standard filesystem. It downloads only the parts of the file that are requested by the OS or applications, allowing you to access Terabytes of data using minimal local disk space.

### Core Features

- **Filesystem Access:** Mount torrents via **FUSE** (Linux/Windows), **WebDAV**, or **HTTP**.
- **On-Demand Downloading:** Only downloads the specific blocks of data being read.
- **Routes:** Organize different sets of torrents into virtual folders.
- **Servers:** Turn any local folder into a live torrent. It monitors the folder and updates the magnet link automatically as files change.
- **qBitTorrent API Compatibility:** Integration with popular automation tools.

## Usage

### 1. Configuration

The application uses a YAML configuration file. You can find an example in `examples/conf_example.yaml`.

### 2. Running

```bash
./distribyted examples/conf_example.yaml
```

### 3. Accessing Files

- **FUSE:** By default, files are mounted to `./distribyted-data/mount` (configurable).
- **WebDAV:** Access via `http://localhost:36911` (Default: admin/admin).
- **HTTP:** Browse files at `http://localhost:4444/fs/`.

### 4. Integration with Radarr/Sonarr

You can add `distribyted` as a qBitTorrent download client:

- **Host:** `localhost`
- **Port:** `4444`
- **Username/Password:** Any (Authentication is currently mocked to always succeed)
- **Category:** Use the name of one of your configured **Routes**.

## qBitTorrent API (v2) Support

The following endpoints are implemented under `/api/v2`:

- `POST /auth/login` - Always returns OK.
- `GET /torrents/info` - Lists torrents in a format compatible with qBitTorrent.
- `POST /torrents/add` - Adds magnet links. The `category` parameter determines the `route`.
- `POST /torrents/delete` - Removes torrents.

## Use Cases

- **Multimedia:** Stream 4K movies directly in VLC without waiting for the full download.
- **Datasets:** Browse massive public datasets and only download the specific files or offsets needed for analysis.
- **Gaming:** Access large ROM collections or game backups directly from the filesystem.
- **Content Sharing:** Use the **Server** feature to instantly share a local folder with anyone via a magnet link.

## Documentation

Check [here][main-url] for further documentation.

## Contributing

Contributions are welcome! Specifically:

- Testing and compatibility improvements for Windows and macOS.
- Enhancements to the Web Dashboard.
- New tutorials and use cases.

## License

Distributed under the GPL3 license. See `LICENSE` for more information.

[main-url]: https://distribyted.com
[releases-shield]: https://img.shields.io/github/v/release/Apollogeddon/distribyted.svg?style=flat-square
[releases-url]: https://github.com/Apollogeddon/distribyted/releases
[docker-pulls-shield]:https://img.shields.io/docker/pulls/Apollogeddon/distribyted.svg?style=flat-square
[docker-pulls-url]:https://hub.docker.com/r/Apollogeddon/distribyted
[contributors-shield]: https://img.shields.io/github/contributors/Apollogeddon/distribyted.svg?style=flat-square
[contributors-url]: https://github.com/Apollogeddon/distribyted/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/Apollogeddon/distribyted.svg?style=flat-square
[forks-url]: https://github.com/Apollogeddon/distribyted/network/members
[stars-shield]: https://img.shields.io/github/stars/Apollogeddon/distribyted.svg?style=flat-square
[stars-url]: https://github.com/Apollogeddon/distribyted/stargazers
[issues-shield]: https://img.shields.io/github/issues/Apollogeddon/distribyted.svg?style=flat-square
[issues-url]: https://github.com/Apollogeddon/distribyted/issues
[license-shield]: https://img.shields.io/github/license/Apollogeddon/distribyted.svg?style=flat-square
[license-url]: https://github.com/Apollogeddon/distribyted/blob/master/LICENSE
[product-screenshot]: mkdocs/docs/images/distribyted.gif
[coveralls-shield]: https://img.shields.io/coveralls/github/Apollogeddon/distribyted?style=flat-square
[coveralls-url]: https://coveralls.io/github/Apollogeddon/distribyted
