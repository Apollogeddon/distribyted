# Architecture

Distribyted is designed as a modular bridge between the BitTorrent protocol and standard filesystem interfaces (FUSE, WebDAV, HTTP). It allows for **random-access reads** over a BitTorrent swarm by treating the torrent data as a lazy-loaded block device.

## Core Components

### 1. Torrent Engine
The core torrent functionality is powered by [anacrolix/torrent](https://github.com/anacrolix/torrent). 
- **Storage Layer**: A custom storage implementation interfaces with the engine to provide piece-level access.
- **Random Access**: Unlike traditional clients that download sequentially or based on piece priority, Distribyted's engine is driven by filesystem read requests. When a file offset is requested, the engine identifies the corresponding BitTorrent pieces and fetches them with high priority.
- **Caching**: Local piece completion is tracked via **BoltDB**, and data is cached in a configurable global file cache.

### 2. Virtual Filesystem (VFS)
The VFS layer maps the flat list of files in a torrent into a hierarchical directory tree.
- **ContainerFs**: The root of the filesystem. It acts as a mount point aggregator that can combine multiple "Routes" (individual torrent collections) into a single unified tree.
- **TorrentFS**: Represents a single torrent's internal structure. It handles metadata and file lookups.
- **Inode Generation**: Inodes are generated dynamically using a combination of a global counter and deterministic hashing of file paths and info-hashes, ensuring consistency across restarts.

### 3. Filesystem Interfaces
Distribyted exposes the VFS through multiple protocols:
- **FUSE (Filesystem in Userspace)**: Using `cgofuse`, the VFS is mounted as a native drive on Linux or Windows (via WinFsp). This provides the highest performance and best application compatibility.
- **WebDAV**: A built-in WebDAV server allows the filesystem to be mounted over the network or in environments where FUSE is unavailable (e.g., some Docker setups).
- **HTTP/httpfs**: A basic web-based file browser for quick access and verification.

### 4. Authentication
The HTTP server (`internal/http`) protects everything except `/login` and static assets (`/assets/*`) behind a qBittorrent-style session cookie (`SID`):
- **Browser clients** (Web UI, HTTPFS) hit a real login page at `/login`; on success a session cookie is set and the browser is redirected back to the page it originally requested.
- **API clients** (Radarr/Sonarr/Prowlarr via the qBittorrent-compatible API, or direct callers of the native `/api/*` routes) authenticate the same way real qBittorrent does: `POST /api/v2/auth/login` with `username`/`password` form fields, which sets the `SID` cookie for subsequent requests. Missing or invalid sessions get `403` on the qBittorrent-compat API (matching real qBittorrent, so *arr clients re-authenticate automatically) or a redirect to `/login` on browser-facing routes.
- Credentials are configured via `http.user`/`http.pass` and compared in constant time (`internal/auth.CredentialsMatch`). **The server fails closed**: if those aren't set and `http.disable_auth: true` isn't explicitly set either, startup is refused (`internal/config.Validate`) rather than silently running unauthenticated.
- WebDAV (`internal/webdav`) is authenticated separately via HTTP Basic Auth against `webdav.user`/`webdav.pass`, with the same fail-closed behavior and constant-time comparison.

## Data Flow: A Read Request

1. **Application**: A media player (like VLC) requests 1MB of data at offset 5GB from a file in the mounted FUSE drive.
2. **FUSE Handler**: The request is intercepted by `cgofuse` and passed to the `ContainerFs`.
3. **VFS**: `ContainerFs` routes the request to the specific `TorrentFS`.
4. **Torrent Engine**: The `TorrentFS` calculates which BitTorrent pieces (e.g., pieces 2000 through 2005) contain the requested byte range.
5. **Network**: The engine prioritizes these pieces and downloads them from the swarm.
6. **Delivery**: As pieces arrive, the data is buffered and returned to the media player.

## Persistence
- **Magnet Database**: All added torrents and routes are stored in a BoltDB database (`magnetdb`), ensuring your library persists after a restart.
- **Piece Completion**: A separate BoltDB database tracks which pieces are already available on disk to avoid redundant downloads.

## Archive Mounting
One of Distribyted's unique features is its ability to "mount" archives (`.zip`, `.rar`, `.7z`) found within torrents.
- When an archive file is detected, the VFS can spawn a sub-filesystem.
- Using specialized libraries (like `rardecode`), it can perform seeking inside compressed files without extracting the entire archive, fetching only the compressed blocks needed for a specific internal file.
