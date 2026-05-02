package log

import (
	"testing"

	"github.com/anacrolix/log"
	zlog "github.com/rs/zerolog/log"
)

func TestBadgerLogger(t *testing.T) {
	bl := &Badger{L: zlog.Logger}
	bl.Errorf("test error %s", "arg")
	bl.Warningf("test warning %s", "arg")
	bl.Infof("test info %s", "arg")
	bl.Debugf("test debug %s", "arg")
}

func TestTorrentLogger(t *testing.T) {
	tl := &Torrent{L: zlog.Logger}
	
	levels := []log.Level{
		log.Debug,
		log.Info,
		log.Warning,
		log.Error,
		log.Critical,
	}

	for _, lv := range levels {
		tl.Handle(log.Record{
			Level: lv,
			Msg:   log.Str("test message"),
		})
	}
}
