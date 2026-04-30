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
}
