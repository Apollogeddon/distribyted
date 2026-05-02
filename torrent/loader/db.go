package loader

import (
	"fmt"
	"path/filepath"
	"time"

	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/dgraph-io/badger/v3"
	"github.com/rs/zerolog/log"
)

var _ LoaderAdder = &DB{}

const (
	routeRootKey = "/route/"
	linkRootKey  = "/link/"
)

type DB struct {
	db    *badger.DB
	close chan struct{}
}

func NewDB(path string) (*DB, error) {
	absPath, _ := filepath.Abs(path)
	fmt.Printf("DB DEBUG: opening database at %s\n", absPath)
	l := log.Logger.With().Str("component", "torrent-store").Logger()

	opts := badger.DefaultOptions(path).
		WithLogger(&dlog.Badger{L: l}).
		WithValueLogFileSize(1<<26 - 1).
		WithSyncWrites(true)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	d := &DB{
		db:    db,
		close: make(chan struct{}),
	}
	go d.runGC()

	return d, nil
}

func (l *DB) runGC() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for {
				if err := l.db.RunValueLogGC(0.5); err != nil {
					break
				}
			}
		case <-l.close:
			return
		}
	}
}

func (l *DB) ListTorrentPaths() (map[string][]string, error) {
	return nil, nil
}

func (l *DB) AddMagnet(r, m string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		spec, err := metainfo.ParseMagnetUri(m)
		if err != nil {
			return err
		}

		ih := spec.InfoHash.HexString()

		rp := routeRootKey + ih + "/" + r
		fmt.Printf("DB DEBUG: adding magnet key: %s\n", rp)
		return txn.Set([]byte(rp), []byte(m))
	})

	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) RemoveFromHash(r, h string) (bool, error) {
	tx := l.db.NewTransaction(true)
	defer tx.Discard()

	var mh metainfo.Hash
	if err := mh.FromHexString(h); err != nil {
		return false, err
	}

	rp := routeRootKey + h + "/" + r
	if _, err := tx.Get([]byte(rp)); err != nil {
		return false, nil
	}

	if err := tx.Delete([]byte(rp)); err != nil {
		return false, err
	}

	return true, tx.Commit()
}

func (l *DB) ListMagnets() (map[string][]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()

	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := []byte(routeRootKey)
	out := make(map[string][]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		k := string(item.Key())
		fmt.Printf("DB DEBUG: found magnet key: %s\n", k)
		// key is /route/<hash>/<route_name>
		// Let's slice manually: k[/route/<hash>/:]
		r := k[len(routeRootKey)+41:] // 40 hex chars + 1 slash

		val, err := item.ValueCopy(nil)
		if err != nil {
			return nil, err
		}
		out[r] = append(out[r], string(val))
	}

	return out, nil
}

func (l *DB) AddLink(oldpath, newpath string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		key := linkRootKey + newpath
		fmt.Printf("DB DEBUG: adding link key: %s\n", key)
		return txn.Set([]byte(key), []byte(oldpath))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) RemoveLink(targetPath string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		key := linkRootKey + targetPath
		return txn.Delete([]byte(key))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) ListLinks() (map[string]string, error) {
	tx := l.db.NewTransaction(false)
	defer tx.Discard()

	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := []byte(linkRootKey)
	out := make(map[string]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		k := string(item.Key())
		fmt.Printf("DB DEBUG: found link key: %s\n", k)
		newpath := k[len(linkRootKey):]

		val, err := item.ValueCopy(nil)
		if err != nil {
			return nil, err
		}
		out[string(val)] = newpath
	}

	return out, nil
}

func (l *DB) Close() error {
	fmt.Printf("DB DEBUG: closing database\n")
	return l.db.Close()
}

func (l *DB) DumpAllKeys() {
	fmt.Println("--- DB DUMP START ---")
	tx := l.db.NewTransaction(false)
	defer tx.Discard()

	it := tx.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	count := 0
	for it.Rewind(); it.Valid(); it.Next() {
		item := it.Item()
		k := item.Key()
		val, _ := item.ValueCopy(nil)
		fmt.Printf("KEY: %s | VAL: %s\n", string(k), string(val))
		count++
	}
	fmt.Printf("--- DB DUMP END (Total: %d keys) ---\n", count)
}
