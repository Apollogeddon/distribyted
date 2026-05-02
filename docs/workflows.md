# Workflows

This guide illustrates how Distribyted operates and how different components interact to provide on-demand torrent access.

## High-Level System Architecture

The following diagram shows the relationship between external interfaces, the core management layers, and the BitTorrent swarm.

```mermaid
graph TD
    subgraph "External Interfaces"
        UI[Web Dashboard]
        API[qBitTorrent API]
        FUSE[FUSE Mount]
        DAV[WebDAV]
    end

    subgraph "Distribyted Core"
        VFS[Virtual Filesystem - VFS]
        TS[Torrent Service]
        SM[Server Mode Manager]
    end

    subgraph "Data & Persistence"
        Engine[anacrolix/torrent Engine]
        Cache[Local Piece Cache]
        DB[BoltDB - Metadata/Magnets]
    end

    UI --> TS
    API --> TS
    FUSE --> VFS
    DAV --> VFS
    VFS --> TS
    TS --> Engine
    TS --> DB
    Engine --> Cache
    SM --> Engine
    SM --> DB
```

---

## Workflow: Adding a Torrent

When you add a torrent via the Web UI or an automation tool like Radarr, the system follows this path:

1.  **Request**: The interface (Web UI or API) sends a magnet link or torrent file to the **Torrent Service**.
2.  **Metadata Fetch**: The system announces to trackers/DHT to find peers and download the torrent metadata (Info Dictionary).
3.  **VFS Integration**: Once metadata is available, the **VFS** dynamically creates a virtual structure for the torrent.
4.  **Availability**: The files instantly appear in the FUSE mount and WebDAV interface as if they were already downloaded.

---

## Workflow: Reading a File (On-Demand)

This is the core "on-demand" workflow where data is streamed from the swarm as requested by an application.

```mermaid
sequenceDiagram
    participant App as Media Player (VLC/Plex)
    participant FUSE as FUSE Driver (cgofuse)
    participant VFS as Virtual Filesystem
    participant Engine as Torrent Engine
    participant Swarm as BitTorrent Swarm

    App->>FUSE: Open file & Read (Offset: 5GB, Length: 1MB)
    FUSE->>VFS: Translated Read Request
    VFS->>VFS: Calculate Torrent Pieces (e.g. #2500-#2505)
    VFS->>Engine: Priority Request for Pieces
    Engine->>Swarm: Download Piece #2500-#2505
    Swarm-->>Engine: Piece Data Received
    Engine-->>VFS: Data Available
    VFS-->>FUSE: Byte Stream
    FUSE-->>App: Data Delivered
```

1.  **Read Request**: An application requests a specific byte range of a file.
2.  **Piece Mapping**: The VFS identifies exactly which BitTorrent pieces contain that data range.
3.  **Swarm Request**: The Torrent Engine requests those pieces with the highest priority from connected peers.
4.  **Streaming**: As data arrives, it is passed back through the VFS to the requesting application.

---

## Command & Action Workflows

The following diagram maps the internal system calls and persistence steps triggered by common external actions.

```mermaid
sequenceDiagram
    participant User as User / Ext Tool
    participant API as API / FUSE Interface
    participant VFS as Virtual Filesystem (VFS)
    participant TS as Torrent Service
    participant DB as BoltDB (Persistence)
    participant Engine as Torrent Engine

    Note over User, Engine: Action: Add Torrent (Magnet/File)
    User->>API: Add Magnet Link (via UI/API)
    API->>TS: AddMagnet(Route, Magnet)
    TS->>DB: Persist Magnet & Route Mapping
    TS->>Engine: Start Metadata Fetch
    Engine-->>TS: Metadata Ready (InfoHash)
    TS->>VFS: Register Torrent in Route
    VFS-->>User: Files visible in /mount/Route/

    Note over User, Engine: Action: Create Virtual Link (ln)
    User->>API: ln /mount/A/file /mount/B/link
    API->>VFS: Link(oldpath, newpath)
    VFS->>VFS: Update Internal Node Tree
    VFS->>TS: Trigger OnLinkAdded Callback
    TS->>DB: Persist Link Mapping

    Note over User, Engine: Action: Remove Torrent
    User->>API: Delete Torrent
    API->>TS: Remove(Hash)
    TS->>DB: Delete Magnet Record
    TS->>VFS: Unregister Torrent
    VFS->>VFS: Surgical Removal of File Nodes
```

### Key Interaction Details
- **Persistence First**: Whenever a torrent or link is added, it is first persisted to **BoltDB** before the VFS is updated. This ensures that the state is recoverable if the application crashes during metadata fetching.
- **Dynamic VFS**: The VFS does not require a restart to show new torrents. The `TorrentService` triggers a callback that injects the new file nodes directly into the active `ContainerFs`.
- **Surgical Removal**: When a torrent is removed, the VFS performs a "surgical removal," only deleting the specific file nodes associated with that torrent's InfoHash, leaving other routes and links intact.

---

## Workflow: On-the-Fly Archive Mounting

Distribyted can treat compressed archives as folders. This involves a "nested" filesystem interaction.

```mermaid
sequenceDiagram
    participant User as User / File Explorer
    participant VFS as Virtual Filesystem (VFS)
    participant AFS as Archive Sub-FS (Zip/Rar)
    participant TFS as Torrent Filesystem
    participant Engine as Torrent Engine

    User->>VFS: Browse into "data.zip/"
    VFS->>VFS: Detect archive extension
    VFS->>AFS: Initialize Archive Handler
    AFS->>TFS: Read Archive Header (at specific offsets)
    TFS->>Engine: Fetch Header Pieces
    AFS-->>User: Show zip contents as folders/files
    
    User->>AFS: Read "internal_file.txt"
    AFS->>TFS: Request compressed byte range
    TFS->>Engine: Fetch specific pieces from swarm
    AFS->>AFS: Decompress stream on-the-fly
    AFS-->>User: Deliver decrypted/decompressed data
```

---

## Workflow: Cache Lifecycle (LRU)

To keep local disk usage low, Distribyted uses a **Least Recently Used (LRU)** cache.

1.  **Incoming Data**: As pieces arrive from the swarm, they are written to the `metadata/cache` folder.
2.  **Usage**: The system tracks which pieces are being accessed by the VFS.
3.  **Capacity Check**: Once the total size of the cache reaches the `global_cache_size` (e.g., 2GB), the eviction process begins.
4.  **Eviction**: The oldest/least accessed pieces are deleted from the disk to make room for new data.
5.  **Metadata Preservation**: Note that **Torrent Metadata** (the file list and piece hashes) is never evicted; only the actual file data pieces are cycled.

