package torrent

import (
	"bytes"
	"encoding/gob"
	"sync"
	"time"

	dlog "github.com/Apollogeddon/distribyted/log"
	"github.com/anacrolix/dht/v2/bep44"
	"github.com/dgraph-io/badger/v3"
	"github.com/rs/zerolog/log"
)

var _ bep44.Store = &FileItemStore{}

type FileItemStore struct {
	ttl       time.Duration
	db        *badger.DB
	closeChan chan struct{}
	inMemory  bool
	closeOnce sync.Once
	closeErr  error
}

func NewFileItemStore(path string, itemsTTL time.Duration) (*FileItemStore, error) {
	l := log.Logger.With().Str("component", "item-store").Logger()
	var opts badger.Options
	if path == "" {
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		opts = badger.DefaultOptions(path)
	}

	opts = opts.WithLogger(&dlog.Badger{L: l}).
		WithValueLogFileSize(1<<26 - 1).
		WithValueThreshold(1024)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	fis := &FileItemStore{
		db:        db,
		ttl:       itemsTTL,
		closeChan: make(chan struct{}),
		inMemory:  path == "",
	}
	if !fis.inMemory {
		_ = db.RunValueLogGC(0.5)
		go fis.runGC()
	}

	return fis, nil
}

func (fis *FileItemStore) runGC() {
	stop := fis.closeChan
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for {
				if err := fis.db.RunValueLogGC(0.5); err != nil {
					break
				}
			}
		case <-stop:
			return
		}
	}
}

func (fis *FileItemStore) Put(i *bep44.Item) error {
	tx := fis.db.NewTransaction(true)
	defer tx.Discard()

	key := i.Target()
	var value bytes.Buffer

	enc := gob.NewEncoder(&value)
	if err := enc.Encode(i); err != nil {
		return err
	}

	e := badger.NewEntry(key[:], value.Bytes()).WithTTL(fis.ttl)
	if err := tx.SetEntry(e); err != nil {
		return err
	}

	return tx.Commit()
}

func (fis *FileItemStore) Get(t bep44.Target) (*bep44.Item, error) {
	tx := fis.db.NewTransaction(false)
	defer tx.Discard()

	dbi, err := tx.Get(t[:])
	if err == badger.ErrKeyNotFound {
		return nil, bep44.ErrItemNotFound
	}
	if err != nil {
		return nil, err
	}
	valb, err := dbi.ValueCopy(nil)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(valb)
	dec := gob.NewDecoder(buf)
	var i *bep44.Item
	if err := dec.Decode(&i); err != nil {
		return nil, err
	}

	return i, nil
}

func (fis *FileItemStore) Del(t bep44.Target) error {
	// ignore this
	return nil
}

func (fis *FileItemStore) Close() error {
	fis.closeOnce.Do(func() {
		close(fis.closeChan)
		fis.closeErr = fis.db.Close()
	})
	return fis.closeErr
}
