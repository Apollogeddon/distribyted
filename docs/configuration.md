# Configuration

Distribyted uses a YAML configuration file to define its behavior, network settings, and data sources. By default, it looks for a file at `./distribyted-data/config/config.yaml`.

## Global Settings

### `http`
Configures the web dashboard and management API.

| Field | Description | Default |
| :--- | :--- | :--- |
| `port` | The port for the HTTP server. | `4444` |
| `ip` | The IP address to bind to. | `0.0.0.0` |
| `httpfs` | Enable/Disable the built-in HTTP file browser. | `true` |

### `webdav`
Configures the WebDAV server for remote filesystem access.

| Field | Description | Default |
| :--- | :--- | :--- |
| `port` | The port for the WebDAV server. | `36911` |
| `user` | Username for authentication. | `admin` |
| `pass` | Password for authentication. | `admin` |

### `fuse`
Configures the FUSE mount settings.

| Field | Description | Default |
| :--- | :--- | :--- |
| `path` | Local directory where the filesystem will be mounted. | `./distribyted-data/mount` |
| `allow_other` | Allow other users to access the mount point. | `false` |

### `torrent`
Core settings for the BitTorrent engine.

| Field | Description | Default |
| :--- | :--- | :--- |
| `global_cache_size` | Maximum size of the file cache in MB. | `2048` (2GB) |
| `metadata_folder` | Path to store metadata, databases, and cache. | `./distribyted-data/metadata` |
| `read_timeout` | Timeout in seconds for filesystem read operations. | `120` |
| `add_timeout` | Timeout in seconds when adding a new torrent (metadata fetch). | `60` |
| `continue_when_add_timeout` | If true, continues even if metadata fetch fails during startup. | `false` |
| `disable_ipv6` | Disable IPv6 support in the torrent engine. | `false` |
| `disable_tcp` | Disable TCP protocol. | `false` |
| `disable_utp` | Disable uTP protocol. | `false` |
| `ip` | Public IP to report to trackers/DHT. | (Auto-detected) |

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
| `torrents` | A list of specific torrents (magnets or local paths). |
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
