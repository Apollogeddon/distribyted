package log

import (
	"fmt"
	"log/slog"

	"github.com/anacrolix/log"
	"github.com/rs/zerolog"
)

var _ log.Handler = &Torrent{}

type Torrent struct {
	L zerolog.Logger
}

func (l *Torrent) Handle(r log.Record) {
	e := l.L.Info()
	switch r.Level {
	case log.Debug:
		e = l.L.Debug()
	case log.Info:
		e = l.L.Debug().Str("error-type", "info")
	case log.Warning:
		e = l.L.Warn()
	case log.Error:
		e = l.L.Warn().Str("error-type", "error")
	case log.Critical:
		e = l.L.Warn().Str("error-type", "critical")
	}

	if e.Enabled() {
		e = withValues(e, r)
	}

	e.Msg(r.Text())
}

// withValues adds the record's values: a slog record's attributes by key under "attrs", and
// any other values, which carry no key we can read, as text under "values".
func withValues(e *zerolog.Event, r log.Record) *zerolog.Event {
	if rec := r.SlogRecord(); rec.Ok && rec.Value.NumAttrs() > 0 {
		attrs := zerolog.Dict()
		rec.Value.Attrs(func(a slog.Attr) bool {
			attrs = attrs.Str(a.Key, a.Value.Resolve().String())
			return true
		})
		return e.Dict("attrs", attrs)
	}

	var values []string
	r.Values(func(v any) bool {
		values = append(values, fmt.Sprint(v))
		return true
	})
	if len(values) > 0 {
		e = e.Strs("values", values)
	}
	return e
}
