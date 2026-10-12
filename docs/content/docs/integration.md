---
title: Integration
weight: 4
---

Distribyted is designed to work seamlessly with existing automation and media tools. This guide explains how to set up these integrations.

## Automation Tools (Radarr, Sonarr, Prowlarr)

Distribyted provides a **qBitTorrent-compatible API (v2)**, allowing it to act as a drop-in replacement for a standard torrent client.

### 1. Connection Settings
In your automation tool (e.g., Radarr), add a new "qBitTorrent" download client with the following settings:

- **Host**: `localhost` (or the IP where Distribyted is running)
- **Port**: `4444` (Default HTTP port)
- **Username**: the value configured under `http.user` in your YAML config
- **Password**: the value configured under `http.pass` in your YAML config

### 2. Categories and Routes
In Radarr/Sonarr, the **Category** field corresponds directly to a **Route** in Distribyted.
- If you set the category to `Movies`, Distribyted will add the torrent to the `Movies` route.
- If the route doesn't exist in your YAML config, Distribyted will create it dynamically.

### 3. Path Mapping
Automation tools expect the download client to report where the files are stored. Distribyted reports the FUSE mount path (e.g., `/distribyted-data/mount/Movies/FileName`).
- Ensure the automation tool has access to the same FUSE mount path or use **Remote Path Mappings** if they are running on different machines/containers.

---

## Media Servers (Plex, Jellyfin, Emby)

Because Distribyted exposes torrents as a standard filesystem, media servers see them as local files.

### 1. Mount the Filesystem
Ensure Distribyted is running and the filesystem is mounted via FUSE.
- **Linux**: Usually at `/distribyted-data/mount` or your configured path.
- **Windows**: Usually mounted as a drive letter or a directory via WinFsp.

### 2. Add Libraries
In your media server (e.g., Plex):
1. Add a new Library (e.g., "Movies").
2. Point the library to the specific route folder inside the mount point (e.g., `/distribyted-data/mount/Movies`).

### 3. Optimization Settings
Media servers often perform "Deep Analysis" or "Thumbnail Generation" which involves reading the entire file.
- **Disable Automatic Analysis**: To prevent Distribyted from downloading the entire torrent library at once, it is highly recommended to disable automatic media analysis and thumbnail generation in your media server.
- **On-Demand Only**: Only allow the media server to analyze a file when it is actually being played.

---

## Health Checks (Docker, Kubernetes)

The HTTP server answers two endpoints for health checks, without logging in and whatever `http.disable_auth` says. They return only `ok` or `not ready` as plain text, and nothing about torrents, routes or paths, and aren't written to the log.

| Endpoint | Answers `200 ok` when | Otherwise |
| :--- | :--- | :--- |
| `GET /healthz` | The process is up and the HTTP server is serving. | No answer |
| `GET /readyz` | The torrent client has started, the configured routes have loaded and, when `fuse` is configured, the FUSE filesystem is mounted. | `503 not ready` |

The HTTP server starts only once the torrent client has started and the routes have loaded, so until then neither endpoint answers. Loading many torrents can take minutes, so give a liveness check a long enough start period (or a Kubernetes startup probe) not to restart distribyted while it loads. `/readyz` turns `503` again if the FUSE filesystem is unmounted.

The binary checks itself, so an image needs no `curl`: `distribyted healthcheck` exits `0` when `/healthz` answers `200`, and `distribyted healthcheck --ready` checks `/readyz`. It finds the server the way distribyted does, from `--http-port` (`DISTRIBYTED_HTTP_PORT`), else the `http` section of the configuration file (`--config`, `DISTRIBYTED_CONFIG`), which it reads but never creates, else port `4444`, and the address from `--http-ip` (`DISTRIBYTED_HTTP_IP`) or `http.ip`, calling `127.0.0.1` when that's `0.0.0.0`. The container image sets `DISTRIBYTED_HTTP_IP` and `DISTRIBYTED_WEBDAV_IP` to `0.0.0.0`, so both are reached through the published ports, and runs `distribyted healthcheck` as its `HEALTHCHECK`, every 30 seconds after a 5-minute start period. It doesn't see the container's command-line arguments, so set a configuration path or port through `DISTRIBYTED_CONFIG` or `DISTRIBYTED_HTTP_PORT` rather than `--config` or `--http-port`.

A Kubernetes pod can use the endpoints directly:

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 4444 }
startupProbe:
  httpGet: { path: /healthz, port: 4444 }
  periodSeconds: 10
  failureThreshold: 60
readinessProbe:
  httpGet: { path: /readyz, port: 4444 }
```

## End-to-End Workflow Example

1. **Prowlarr** finds a 4K movie and sends the magnet link to **Radarr**.
2. **Radarr** sends the magnet to **Distribyted** via the qBitTorrent API, using the category `Movies`.
3. **Distribyted** fetches the metadata and adds the movie to the `/Movies` virtual folder.
4. **Plex** sees a new file in `/distribyted-data/mount/Movies`.
5. When you click **Play** in Plex:
    - Plex requests the first few megabytes of the file.
    - Distribyted's VFS intercepts the request.
    - The Torrent Engine fetches those specific pieces from the swarm.
    - The movie starts playing almost instantly, while the rest of the file remains in the cloud.
