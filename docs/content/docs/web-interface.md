---
title: Web interface
weight: 2
---

The web interface is at `http://localhost:4444` by default (`http.port` in the
[configuration](../configuration/)). Sign in with `http.user` and `http.pass`. It follows
your system's light or dark setting.

A banner at the top of every page says when streaming is in trouble: a read that has waited
more than ten seconds for data, a file playing from a torrent with no peers left, reads
that gave up in the last hour, or links still waiting for their torrents a couple of minutes
after a start. Each has a link to where to look next.

## Dashboard

The dashboard opens with a sentence on what's playing, and names the first stream in
trouble. Below it, one line of figures: download and upload speed with the last minute's
trend, how many streams are playing, how many torrents are short of seeders, how many links
are waiting or missing, and how full the piece cache is. Each of the last three opens the
page with the detail.

**Now playing** lists the files being read, as a player streams them. Each has a rail like
a player's: what has played, in the text colour; what's cached ahead of it, in cyan; and,
under it, which parts of the whole file are cached. For a video or audio file (MKV, WebM,
MP4, MOV, M4A, FLAC or MP3), distribyted reads how long it plays from its header, once, and
shows the position and what's buffered as times; for anything else, such as a book, a PDF
or a ROM, they're in bytes. The position is estimated from how far into the file the player
reads, so it's approximate for a file whose bitrate varies. A stream whose read has waited
more than two seconds, or whose torrent has no peers left with little cached ahead, says why.

Colours keep one meaning across the interface: indigo for what you can press or follow,
cyan for cached data, and green, amber and red for health. Paused and connecting are grey,
as they need nothing from you.

{{< screenshot name="dashboard" alt="The dashboard" >}}

## Routes

Each route is a folder of torrents. A torrent's row shows its health, how many of its
pieces are here, and its peers. **Add magnet** adds a torrent to a route, and the bin
button removes one.

The search box above the list keeps the torrents whose name has every word typed, and the
menu beside it orders them by name, by how much they're moving, with those needing
attention first, or newest first. Both are part of the page's address, so a reload or a
bookmark keeps them.

A torrent's name opens its own page: its files, whether each is ready to play, and the
links that show each one, so you can see what a library would lose before deleting it. Its
**Peers** list shows each connected peer's address, client, how it was found, how much of the
torrent it has, and its speeds, seeders first. Buttons at the top download the start and end
of every file, free its cache, or delete it, and its magnet link can be copied.

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

When an episode that's been played is 95% through, the next one, the next file in the
folder it's linked into (or in its torrent's own folder, for a season pack), has its first
five minutes and its end downloaded, so it starts at once and plays while the rest arrives.
Five minutes is worked out from the playing episode's size and duration, from 64 MB to
1 GB; without a duration it's 5% of the file, up to 512 MB. Played means read on from
where it was for at least 30% of the file, and, when its duration is known, no faster than
twice real time: a library app reading the last minutes for the credits, or reading the
whole file at full speed, doesn't get the next episode downloaded.

{{< screenshot name="files" alt="The Files page" >}}

## Links

A link shows a file at a second path, such as a film filed the way your library names it.
In **Add link**, type part of a file's name or folder to search the mount, and pick the
file from the matches; every word typed has to appear in its path. Archives are found as
files, not searched inside.

Links, and the folders and files made in the mount, come back after a restart. A link
whose torrent is still loading appears as soon as the torrent does.

Until then it's marked **Waiting for its torrent**; a link whose path isn't in the mount and
isn't waiting for anything is marked **Missing**. The line above the list counts both, and
**Needs attention** in the menu beside the search box shows only those links.

{{< screenshot name="links" alt="The Links page" >}}

## Cache

What the piece cache (`global_cache_size`) holds, torrent by torrent: how much of it each
has, and when it was last read. The pieces read longest ago are evicted first as the cache
fills, so a film that played yesterday may need downloading again today. **Free** removes a
torrent's pieces now; they download again when it's next played.

By default a tenth of the cache (`protected_cache_percent`) is kept for the start and end of
every file large enough to play, which only other files' starts can evict, so a film played
through doesn't make everything else wait for peers again. The figure under the cache's size
says how much of that share is in use.

**Downloaded** says what was downloaded in the last day and since distribyted started, and
why: read by a player or another app through the mount, linked into a library, got ready as
the next episode, or prepared by hand. A row notes how much of it was downloaded a second
time because the cache had evicted it; much of that means the cache is too small. Under it,
how long the latest files opened waited for their first byte.

## Servers

Local folders shared as torrents, each with a magnet link that follows the folder's
contents.

{{< screenshot name="servers" alt="The Servers page" >}}

## Logs

The newest entries first, filtered by level. New entries appear as they're written. The
search box keeps the entries holding every word typed, in their message or fields, such as
a film's name, a path or a torrent's hash.

{{< screenshot name="logs" alt="The Logs page" >}}

## On a phone

Below 900 pixels wide the sidebar becomes a menu.

{{< screenshot name="mobile" alt="The Routes page and the menu on a phone" >}}
