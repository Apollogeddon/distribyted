package loader

type Loader interface {
	ListMagnets() (map[string][]string, error)
	ListTorrentPaths() (map[string][]string, error)
}

type LoaderAdder interface {
	Loader

	RemoveFromHash(r, h string) (bool, error)
	AddMagnet(r, m string) error

	AddLink(oldpath, newpath string) error
	RemoveLink(path string) error
	ListLinks() (map[string]string, error)

	// SaveInfo keeps a torrent's info dictionary, so that re-adding it, as every start
	// does, needn't wait for a peer to send it. LoadInfo returns it, and ForgetInfo drops
	// it once the torrent is gone from every route. Hashes are hex v1 info hashes.
	SaveInfo(hash string, info []byte) error
	LoadInfo(hash string) ([]byte, bool)
	ForgetInfo(hash string) error

	// SavePeers keeps the addresses of a torrent's peers that sent it data, to try first
	// when it next needs peers. LoadPeers returns them, and ForgetPeers drops them.
	SavePeers(hash string, addrs []string) error
	LoadPeers(hash string) []string
	ForgetPeers(hash string) error

	// SavedHashes lists the torrents something is saved for, so that what's kept for a
	// torrent no longer loaded from anywhere can be forgotten.
	SavedHashes() ([]string, error)
}
