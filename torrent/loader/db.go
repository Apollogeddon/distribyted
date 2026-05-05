package loader

import (
	"fmt"
	"path"
	"path/filepath"
	"sync"
	"time"

	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/dgraph-io/badger/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var _ LoaderAdder = &DB{}

const (
	routeRootKey = "/route"
	linkRootKey  = "/link"
)

type DB struct {
	db        *badger.DB
	close     chan struct{}
	inMemory  bool
	closeOnce sync.Once
	closeErr  error
	log       zerolog.Logger
}

func NewDB(path string) (*DB, error) {
	l := log.Logger.With().Str("component", "torrent-store").Logger()
	var opts badger.Options
	if path == "" {
		fmt.Printf("DB DEBUG: opening in-memory database\n")
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		absPath, _ := filepath.Abs(path)
		fmt.Printf("DB DEBUG: opening database at %s\n", absPath)
		opts = badger.DefaultOptions(path)
	}

	opts = opts.WithLogger(&dlog.Badger{L: l}).
		WithValueLogFileSize(1<<26 - 1).
		WithValueThreshold(1024).
		WithSyncWrites(true)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	d := &DB{
		db:       db,
		close:    make(chan struct{}),
		inMemory: path == "",
	}
	if !d.inMemory {
		go d.runGC()
	}

	return d, nil
}

func (l *DB) runGC() {
	stop := l.close
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
		case <-stop:
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

		rp := path.Join(routeRootKey, ih, r)
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

	rp := path.Join(routeRootKey, h, r)
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

	prefix := []byte(path.Join(routeRootKey, ""))
	out := make(map[string][]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		k := string(item.Key())
		l.log.Debug().Str("key", k).Msg("found magnet key")
		// key is /route/<hash>/<route_name>
		// routeRootKey + "/" + hash(40) + "/"
		r := k[len(routeRootKey)+42:] 

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
		key := path.Join(linkRootKey, newpath)
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
		key := path.Join(linkRootKey, targetPath)
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

	prefix := []byte(path.Join(linkRootKey, ""))
	out := make(map[string]string)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		k := string(item.Key())
		fmt.Printf("DB DEBUG: found link key: %s\n", k)

		val, err := item.ValueCopy(nil)
		if err != nil {
			return nil, err
		}

		newpath := k[len(linkRootKey)+1:]
		if newpath == "" {
			continue
		}
		out[newpath] = string(val)
	}

	return out, nil
}

func (l *DB) Close() error {
	l.closeOnce.Do(func() {
		fmt.Printf("DB DEBUG: closing database\n")
		if l.close != nil {
			close(l.close)
		}
		l.closeErr = l.db.Close()
	})
	return l.closeErr
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
