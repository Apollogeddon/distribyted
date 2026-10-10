---
title: Getting Started
weight: 1
---

distribyted is a torrent client that presents torrents as files. When an application
reads a file, distribyted fetches only the pieces that read covers, and keeps them in a
local cache.

{{< screenshot name="dashboard" alt="The distribyted dashboard: download and upload speed, cache use and a speed chart" >}}

The [web interface](web-interface/) shows what's happening and lets you manage torrents,
files and links.

## How it works

1. The virtual filesystem works out which pieces of the torrent a read needs.
2. The torrent engine asks the swarm for only those pieces.
3. The data goes straight to the application that asked for it.

## Run it

Download a binary from the [releases page](https://github.com/Apollogeddon/distribyted/releases),
or run the container image `ghcr.io/apollogeddon/distribyted`. Then start it with a
configuration file:

```bash
./distribyted --config examples/conf_example.yaml
```

If the file doesn't exist, distribyted writes one from its built-in template. See
[Configuration](configuration/) for every setting.

{{< callout type="warning" >}}
The generated configuration logs in with `admin`/`admin`. Change both passwords before
the web interface or WebDAV can be reached from another machine.
{{< /callout >}}

## Reach your files

| Interface | Default address |
| --- | --- |
| FUSE | `./distribyted-data/mount` |
| WebDAV | `http://localhost:36911` |
| Web interface | `http://localhost:4444` |

To let Radarr or Sonarr add torrents, see [Integration](integration/).
