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
}

type Error struct {
	Error string `json:"error"`
}
