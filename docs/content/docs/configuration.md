---
title: Configuration
weight: 3
---

Distribyted uses a YAML configuration file to define its behavior, network settings, and data sources. By default, it looks for a file at `./distribyted-data/config/config.yaml`.

## Global Settings

### `http`
Configures the web dashboard and management API.

| Field | Description | Default |
| :--- | :--- | :--- |
| `port` | The port for the HTTP server. The `--http-port` flag or `DISTRIBYTED_HTTP_PORT` overrides it. | `4444` |
| `ip` | The IP address to bind to. `127.0.0.1` serves this machine only; `0.0.0.0` serves every address, so use it only on a network you trust or behind a proxy with TLS, as logins travel in plain HTTP. The `--http-ip` flag or `DISTRIBYTED_HTTP_IP` overrides it. | `127.0.0.1` (the container image sets `0.0.0.0`) |
| `httpfs` | Enable/Disable the built-in HTTP file browser. | `true` |
| `user` | Username for the web interface, native API and qBittorrent-compatible API. Required unless `disable_auth` is `true`. | `admin` in the generated configuration |
| `pass` | Password for the web interface, native API and qBittorrent-compatible API. Required unless `disable_auth` is `true`: an empty one is rejected at startup. | A random one, written to the generated configuration on first run |
| `disable_auth` | Set to `true` to run the HTTP interface with no authentication at all. | `false` |
| `pprof` | Expose `net/http/pprof` under `/debug/pprof`, behind the same session auth as the dashboard. Diagnostic tool — leave off unless actively investigating goroutine/memory growth (see `read_timeout` below). | `false` |

{{< callout type="warning" >}}
**Addresses and logins:** a configuration that names `ip: "0.0.0.0"` for `http` or `webdav` listens on every address, and distribyted logs a warning at startup for each. Change it to `127.0.0.1` unless other machines need it. A login left as `admin`/`admin` is only warned about at startup too, not refused: change `http.pass` and `webdav.pass`.
{{< /callout >}}

### `webdav`
Configures the WebDAV server for remote filesystem access.

| Field | Description | Default |
| :--- | :--- | :--- |
| `port` | The port for the WebDAV server. The `--webdav-port` flag or `DISTRIBYTED_WEBDAV_PORT` overrides it. | `36911` |
| `ip` | The IP address to bind to. Basic auth travels in plain HTTP, so keep `127.0.0.1` unless other machines need WebDAV. The `--webdav-ip` flag or `DISTRIBYTED_WEBDAV_IP` overrides it. After 5 failed logins from one address, it waits before trying again, longer after each further failure. | `127.0.0.1` (the container image sets `0.0.0.0`) |
| `user` | Username for authentication. Required. | `admin` in the generated configuration |
| `pass` | Password for authentication. Required: an empty one is rejected at startup. | A random one (the same as `http.pass`), written to the generated configuration on first run |

### `fuse`
Configures the FUSE mount settings.

| Field | Description | Default |
| :--- | :--- | :--- |
| `path` | Local directory where the filesystem will be mounted. | `./distribyted-data/mount` |
| `allow_other` | Allow other users to access the mount point. Needs `user_allow_other` in `/etc/fuse.conf`. The `--fuse-allow-other` flag or `DISTRIBYTED_FUSE_ALLOW_OTHER` turns it on too. | `false`, but `true` in the generated configuration |

### `torrent`
Core settings for the BitTorrent engine.

| Field | Description | Default |
| :--- | :--- | :--- |
| `global_cache_size` | Maximum size of the piece cache in MB, on every platform; the pieces read longest ago are evicted past it. Pieces still downloading are kept apart, in `cache-partial`, and don't count: evicting one before it finished made it fail its hash check, and the peer that sent it was banned. `-1` means no limit. A cache already larger than this is trimmed to this size on start. | `2048` (2GB) |
| `archive_extract_limit` | Largest file inside a `.zip`, `.rar` or `.7z`, in MB, that can be read. Each one is extracted to a temporary file as it's read, so this bounds the disk one archive entry can use. Reading a bigger one fails (`EFBIG` through FUSE). `-1` means no limit. | `4096` (4GB) |
| `metadata_folder` | Path to store metadata, databases, and cache. The torrent database (`magnetdb`) keeps each torrent's magnet, its metadata (file list and piece hashes), so a restart lists its files without waiting for peers, and the addresses of up to 20 peers that sent it data, which it tries first when it next needs data. It sits outside the cache: `global_cache_size` doesn't count it and cache eviction never removes it. A torrent's metadata and peers are deleted when the torrent is removed, and on startup for any torrent no longer in the configuration, a watched folder or the database. | `./distribyted-data/metadata` |
| `read_timeout` | Seconds a filesystem read may go without making forward progress before it's abandoned and fails. Resets on progress rather than being a total time budget, so a legitimately slow read on a thin swarm can still take longer than this in total — it only bounds *stalls*. Any read that blows this deadline is guaranteed to return within it rather than hang the mount, even if the underlying torrent library never would on its own (see `internal/fs/torrent.go`'s `readAtWrapper` doc comment). | `120` |
| `add_timeout` | Timeout in seconds when adding a new torrent (metadata fetch). | `60` |
| `continue_when_add_timeout` | If true, continues even if metadata fetch fails during startup. | `false`, but `true` in the generated configuration |
| `disable_ipv6` | Disable IPv6 support in the torrent engine. | `false` |
| `disable_tcp` | Disable TCP protocol. | `false` |
| `disable_utp` | Disable uTP protocol. | `false` |
| `disable_dht` | Disable the DHT, so peers are found only through trackers and peer exchange. | `false` |
| `disable_upnp` | Don't ask the router to forward the listening port (UPnP and NAT-PMP). | `false` |
| `listen_port` | Port the torrent engine listens on for peers, over TCP and uTP. `-1` picks a random free port. | `42069` |
| `seed` | Keep uploading a torrent to other peers once it's complete, which `servers` need to share their folders. With it off, `servers` still make magnets, but their content isn't shared with the swarm. | `false` |
| `ip` | Public IP to report to trackers/DHT. | (Auto-detected) |
| `max_conns_per_torrent` | Maximum peer connections per torrent, and how many it may be dialling at once. Distribyted defaults to half the torrent library's 50 established, and the library's own 25 dialling: in a swarm with many dead addresses each dial waits out its timeout, so fewer at once is slower to reach the seeders. Raise it on a fast connection if a torrent has many available peers. | `25` |
| `responsive_reads` | Return reads as soon as their data has arrived, instead of waiting for the whole covering piece to finish downloading *and* pass hash verification. Substantially reduces time-to-first-byte, especially with large piece lengths (see `docs/benchmarking.md`) — but bytes may reach a reader before their integrity against the torrent's hash has been confirmed. | `false` |

### `log`
Configures application logging.

| Field | Description |
| :--- | :--- |
| `debug` | Enable verbose debug logging. |
| `path` | Directory where log files will be saved. |
| `max_size` | Maximum size of each log file in MB. |
| `max_backups` | Number of old log files to keep. |
| `max_age` | Number of days to keep log files. |

---

## Data Sources

### `routes`
Routes allow you to organize torrents into virtual folders.

```yaml
routes:
  - name: "Movies"
    torrents:
      - magnet_uri: "magnet:?xt=urn:btih:..."
      - torrent_path: "./torrents/example.torrent"
  - name: "Linux ISOs"
    torrent_folder: "/path/to/folder/with/torrents"
```

| Field | Description |
| :--- | :--- |
| `name` | The name of the virtual folder in the filesystem. |
| `torrents` | A list of specific torrents, each a `magnet_uri` or a `torrent_path` to a local `.torrent` file. |
| `torrent_folder` | A local directory to watch for `.torrent` files to add automatically. |

### `servers`
Servers turn a local folder into a live torrent.

```yaml
servers:
  - name: "My Shared Folder"
    path: "/home/user/share"
    trackers:
      - "udp://tracker.opentrackr.org:1337/announce"
```

| Field | Description |
| :--- | :--- |
| `name` | Name of the server. |
| `path` | Local folder to monitor and share. |
| `trackers` | List of tracker URLs to announce to. |
| `tracker_url` | Optional URL to fetch a dynamic list of trackers. |
| `watcher_interval` | Seconds between checks of the folder for changes; a change makes a new magnet. | `5` |
