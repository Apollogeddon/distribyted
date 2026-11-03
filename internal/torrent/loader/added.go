package loader

import (
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// When each link was made is kept beside it, under /added/<newpath>, as Unix seconds, so the
// dashboard can list what an *arr app imported lately. Links restored after a start keep the
// time they were first made; links made before this was kept have none.

const addedRootKey = "/added"

// SetLinkAdded records that the link at newpath was made at t.
func (l *DB) SetLinkAdded(newpath string, t time.Time) error {
	err := l.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(path.Join(addedRootKey, newpath)), []byte(strconv.FormatInt(t.Unix(), 10)))
	})
	if err != nil {
		return err
	}
	return l.db.Sync()
}

// ForgetLinkAdded drops the time of the link at newpath.
func (l *DB) ForgetLinkAdded(newpath string) error {
	return l.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(path.Join(addedRootKey, newpath)))
	})
}

// LinksAdded lists when each link was made, by its path (with its leading /).
func (l *DB) LinksAdded() (map[string]time.Time, error) {
	out := map[string]time.Time{}
	err := l.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(addedRootKey + "/")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			v, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			secs, err := strconv.ParseInt(string(v), 10, 64)
			if err != nil {
				continue
			}
			out["/"+strings.TrimPrefix(k, addedRootKey+"/")] = time.Unix(secs, 0)
		}
		return nil
	})
	return out, err
}
