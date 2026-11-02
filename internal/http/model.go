package http

type RouteAdd struct {
	Magnet string `json:"magnet" binding:"required"`
}

type LinkAdd struct {
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

type Link struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	IsDir   bool   `json:"isDir"`
	// IsFile marks an empty file made through the mount, which, like a folder, shows
	// nothing else and has no OldPath.
	IsFile bool `json:"isFile"`
	// Route is the name of the route OldPath falls under, resolved by
	// longest-prefix match against live route names, or "" if OldPath
	// doesn't fall under any known route (e.g. a link pointing at another
	// link). Empty for directory links (IsDir), which have no OldPath.
	Route string `json:"route"`
}

type FSEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	Hash  string `json:"hash"`
	// Owned reports whether this entry can be deleted/renamed through the
	// file browser (a link, or a Mkdir/Create'd path) as opposed to being
	// route-mounted torrent content, which must be managed from Routes.
	Owned bool `json:"owned"`
	// Ready, for a torrent's file, reports whether its start and end are in the piece cache,
	// so a player can open it and start playing without waiting on a peer.
	Ready *bool `json:"ready,omitempty"`
}

type MkdirRequest struct {
	Path string `json:"path" binding:"required"`
}

type RenameRequest struct {
	OldPath string `json:"old_path" binding:"required"`
	NewPath string `json:"new_path" binding:"required"`
}

type Error struct {
	Error string `json:"error"`
}
