package log

import (
	"bytes"
	"testing"

	"github.com/anacrolix/log"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
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

func TestTorrentLogger_Values(t *testing.T) {
	var buf bytes.Buffer
	tl := &Torrent{L: zerolog.New(&buf)}

	tl.Handle(log.Record{Level: log.Warning, Msg: log.Str("plain").WithValues("peer-1", 42)})
	assert.JSONEq(t, `{"level":"warn","values":["peer-1","42"],"message":"plain"}`, buf.String())

	// the way the torrent client logs through slog
	buf.Reset()
	l := log.NewLogger()
	l.SetHandlers(tl)
	l.Slogger().Warn("from slog", "infohash", "abc", "pieces", 3)
	assert.JSONEq(t, `{"level":"warn","attrs":{"infohash":"abc","pieces":"3"},"message":"from slog"}`, buf.String())

	buf.Reset()
	tl.Handle(log.Record{Level: log.Warning, Msg: log.Str("bare")})
	assert.JSONEq(t, `{"level":"warn","message":"bare"}`, buf.String())
}
