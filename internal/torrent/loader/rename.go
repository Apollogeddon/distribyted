package loader

import (
	"path"
	"strings"

	"github.com/dgraph-io/badger/v4"

	"github.com/Apollogeddon/distribyted/internal/config"
)

// A route's name is its folder at the root of the mount, and the database keeps it in two
// places: each torrent added to the route is stored under it, and each link records the path
// it shows, inside the route's folder. Renaming a route in the configuration alone left both
// under the old name, which the database then brought back as a route of its own beside the
// new one. RenameRoute moves them.

// RenameRoute moves every torrent stored under the route from to the route to, and points every
// link, folder and file made in the mount that's at or shows a path in /from at the same path
// in /to. A record already at a path in /to is kept, and the one it would replace dropped. It
// returns how many torrents and link records it changed; once done there's nothing left to
// move, so it's safe to run at every start.
func (l *DB) RenameRoute(from, to string) (torrents, links int, err error) {
	if err := config.ValidateRouteName(from); err != nil {
		return 0, 0, err
	}
	if err := config.ValidateRouteName(to); err != nil {
		return 0, 0, err
	}
	if from == to {
		return 0, 0, nil
	}

	type move struct {
		oldKey, newKey string
		val            []byte
	}
	var moves []move
	err = l.db.View(func(txn *badger.Txn) error {
		exists := func(key string) bool {
			_, err := txn.Get([]byte(key))
			return err == nil
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte(routeRootKey + "/")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			// /route/<hash>/<route>
			k := string(it.Item().Key())
			hash, route, ok := strings.Cut(strings.TrimPrefix(k, routeRootKey+"/"), "/")
			if !ok || route != from {
				continue
			}
			val, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			moves = append(moves, move{k, path.Join(routeRootKey, hash, to), val})
			torrents++
		}

		prefix = []byte(linkRootKey + "/")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			v, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			newpath, ok := renamedPath(strings.TrimPrefix(k, linkRootKey), from, to)
			newKey := k
			if ok {
				newKey = path.Join(linkRootKey, newpath)
				if exists(newKey) {
					// what's already in /to stays; the record that would replace it goes
					moves = append(moves, move{oldKey: k})
					links++
					continue
				}
			}
			newVal := v
			// a folder's or an empty file's mark isn't a path
			if src, ok := renamedPath(string(v), from, to); ok && string(v) != "/" {
				newVal = []byte(src)
			}
			if newKey == k && string(newVal) == string(v) {
				continue
			}
			moves = append(moves, move{k, newKey, newVal})
			links++
		}
		return nil
	})
	if err != nil || len(moves) == 0 {
		return 0, 0, err
	}

	// one write batch, not a transaction, as a large library outgrows one; a start that stops
	// part way through finds what's left and moves it the next time
	wb := l.db.NewWriteBatch()
	defer wb.Cancel()
	for _, m := range moves {
		if m.newKey != "" {
			if err := wb.Set([]byte(m.newKey), m.val); err != nil {
				return 0, 0, err
			}
		}
		if m.newKey != m.oldKey {
			if err := wb.Delete([]byte(m.oldKey)); err != nil {
				return 0, 0, err
			}
		}
	}
	if err := wb.Flush(); err != nil {
		return 0, 0, err
	}
	return torrents, links, l.db.Sync()
}

// renamedPath is p in /to when it's /from or inside it.
func renamedPath(p, from, to string) (string, bool) {
	old := "/" + from
	if p == old {
		return "/" + to, true
	}
	if rest, ok := strings.CutPrefix(p, old+"/"); ok {
		return "/" + to + "/" + rest, true
	}
	return p, false
}
