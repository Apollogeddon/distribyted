---
title: Web interface
weight: 2
---

The web interface is at `http://localhost:4444` by default (`http.port` in the
[configuration](../configuration/)). Sign in with `http.user` and `http.pass`. It follows
your system's light or dark setting, and the button at the foot of the sidebar switches
between them.

## Dashboard

Download and upload speed, the cache in use, and a chart of the last minute's speeds.

![The dashboard](/images/screenshots/dashboard-dark.png)

## Routes

Each route is a folder of torrents. A torrent's row shows its health, how many of its
pieces are here, and its peers. **Add magnet** adds a torrent to a route, and the bin
button removes one.

![The Routes page](/images/screenshots/routes-dark.png)

A magnet link that can't be added says why, without closing the dialog.

![Adding a magnet link that isn't valid](/images/screenshots/add-magnet-dark.png)

## Files

Everything the mounts show. Download a file, make a folder, rename or delete what you
added, or link a file to a second path. Files inside a torrent are managed from Routes.

![The Files page](/images/screenshots/files-light.png)

## Links

A link shows a file at a second path, such as a film filed the way your library names it.

![The Links page](/images/screenshots/links-light.png)

## Servers

Local folders shared as torrents, each with a magnet link that follows the folder's
contents.

![The Servers page](/images/screenshots/servers-dark.png)

## Logs

The newest entries first, filtered by level. New entries appear as they're written.

![The Logs page](/images/screenshots/logs-dark.png)

## On a phone

Below 900 pixels wide the sidebar becomes a menu.

![The Routes page and the menu on a phone](/images/screenshots/mobile.png)
