package loader

import (
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"

	dlog "github.com/Apollogeddon/distribyted/internal/log"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var _ LoaderAdder = &DB{}

const (
	routeRootKey = "/route"
	linkRootKey  = "/link"
	infoRootKey  = "/info"
	peersRootKey = "/peers"
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
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		opts = badger.DefaultOptions(path)
	}

	opts = opts.WithLogger(&dlog.Badger{L: l}).
		WithValueLogFileSize(1<<26 - 1).
		WithValueThreshold(1024).
		WithSyncWrites(true).
		WithBlockCacheSize(8 << 20).
		WithNumMemtables(1).
		WithMemTableSize(16 << 20)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	d := &DB{
		db:       db,
		close:    make(chan struct{}),
		inMemory: path == "",
		log:      l,
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
	if err := config.ValidateRouteName(r); err != nil {
		return err
	}

	err := l.db.Update(func(txn *badger.Txn) error {
		spec, err := metainfo.ParseMagnetUri(m)
		if err != nil {
			return err
		}

		ih := spec.InfoHash.HexString()

		rp := path.Join(routeRootKey, ih, r)
		return txn.Set([]byte(rp), []byte(m))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) RemoveFromHash(r, h string) (bool, error) {
	if err := config.ValidateRouteName(r); err != nil {
		return false, err
	}

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
		// a key written before route names were validated, such as /route/<hash> for the
		// route ".", has no route segment; skip it rather than fail every startup
		if len(k) <= len(routeRootKey)+42 || k[len(routeRootKey)+41] != '/' {
			l.log.Warn().Str("key", k).Msg("skipping a stored torrent with an invalid route")
			continue
		}
		r := k[len(routeRootKey)+42:]
		if config.ValidateRouteName(r) != nil {
			l.log.Warn().Str("key", k).Msg("skipping a stored torrent with an invalid route")
			continue
		}

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

func (l *DB) SaveInfo(hash string, info []byte) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(path.Join(infoRootKey, hash)), info)
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) LoadInfo(hash string) ([]byte, bool) {
	var info []byte
	err := l.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(path.Join(infoRootKey, hash)))
		if err != nil {
			return err
		}
		info, err = item.ValueCopy(nil)
		return err
	})
	return info, err == nil
}

func (l *DB) ForgetInfo(hash string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(path.Join(infoRootKey, hash)))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) SavePeers(hash string, addrs []string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(path.Join(peersRootKey, hash)), []byte(strings.Join(addrs, "\n")))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) LoadPeers(hash string) []string {
	var addrs []string
	_ = l.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(path.Join(peersRootKey, hash)))
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			if len(v) > 0 {
				addrs = strings.Split(string(v), "\n")
			}
			return nil
		})
	})
	return addrs
}

func (l *DB) ForgetPeers(hash string) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(path.Join(peersRootKey, hash)))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

func (l *DB) SavedHashes() ([]string, error) {
	var hashes []string
	err := l.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for _, root := range []string{infoRootKey, peersRootKey} {
			prefix := []byte(root + "/")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				h := string(it.Item().Key()[len(prefix):])
				if !slices.Contains(hashes, h) {
					hashes = append(hashes, h)
				}
			}
		}
		return nil
	})
	return hashes, err
}

func (l *DB) Close() error {
	l.closeOnce.Do(func() {
		if l.close != nil {
			close(l.close)
		}
		l.closeErr = l.db.Close()
	})
	return l.closeErr
}
