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

{{< screenshot name="dashboard" alt="The dashboard" >}}

## Routes

Each route is a folder of torrents. A torrent's row shows its health, how many of its
pieces are here, and its peers. **Add magnet** adds a torrent to a route, and the bin
button removes one.

{{< screenshot name="routes" alt="The Routes page" >}}

A magnet link that can't be added says why, without closing the dialog.

{{< screenshot name="add-magnet" alt="Adding a magnet link that isn't valid" >}}

## Files

Everything the mounts show. Download a file, make a folder, rename or delete what you
added, or link a file to a second path. Files inside a torrent are managed from Routes.

Each torrent's file says whether it's **Ready to play**: its start and end, which a player
reads before it shows a frame, are in the piece cache. Select it to see how much of the
file is cached and how many peers its torrent has, and to download its start and end, or
the whole file, ahead of time. A file over half the piece cache (`global_cache_size`)
can't be downloaded whole, as it would push everything else out.

A file linked into a library, as Radarr's and Sonarr's imports do, has its start and end
downloaded straight away, so its first play starts at once.

{{< screenshot name="files" alt="The Files page" >}}

## Links

A link shows a file at a second path, such as a film filed the way your library names it.
In **Add link**, type part of a file's name or folder to search the mount, and pick the
file from the matches; every word typed has to appear in its path. Archives are found as
files, not searched inside.

Links, and the folders and files made in the mount, come back after a restart. A link
whose torrent is still loading appears as soon as the torrent does.

{{< screenshot name="links" alt="The Links page" >}}

## Servers

Local folders shared as torrents, each with a magnet link that follows the folder's
contents.

{{< screenshot name="servers" alt="The Servers page" >}}

## Logs

The newest entries first, filtered by level. New entries appear as they're written.

{{< screenshot name="logs" alt="The Logs page" >}}

## On a phone

Below 900 pixels wide the sidebar becomes a menu.

{{< screenshot name="mobile" alt="The Routes page and the menu on a phone" >}}
