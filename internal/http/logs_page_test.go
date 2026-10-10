package http

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "distribyted.log")
	require.NoError(t, os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return p
}

func TestLogsPage(t *testing.T) {
	logPath := writeLog(t,
		`{"level":"info","component":"torrent-service","time":1791547356,"message":"torrent added","route":"films","name":"<b>x</b>"}`,
		`not json at all`,
		`{"level":"warn","time":1791547357,"message":"seeding is disabled","count":3}`,
		`{"level":"error","component":"http","time":1791547358,"message":"failed"}`,
	)
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, logPath, routesConf(), "", nil)
	require.NoError(t, err)

	w := get(r, "/logs")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// newest first
	require.Less(t, strings.Index(body, ">failed"), strings.Index(body, "torrent added"))
	require.Contains(t, body, `<span class="level level-warn">warn</span>`)
	require.Contains(t, body, "<dt>route</dt><dd>films</dd>")
	require.Contains(t, body, "<dt>count</dt><dd>3</dd>")
	require.Contains(t, body, "&lt;b&gt;x&lt;/b&gt;", "log values are text")
	require.Contains(t, body, "not json at all")

	w = get(r, "/logs/list?level=warn")
	require.Equal(t, http.StatusOK, w.Code)
	body = w.Body.String()
	require.Contains(t, body, "seeding is disabled")
	require.NotContains(t, body, "torrent added")
	require.Contains(t, body, `aria-current="true"`)

	// an unknown level shows everything
	require.Contains(t, get(r, "/logs/list?level=bogus").Body.String(), "torrent added")
}

// TestTailLog: only the end of a large log is read, and the line cut in half by where
// reading starts is dropped rather than shown as garbage.
func TestTailLog(t *testing.T) {
	var lines []string
	for i := 0; i < 5000; i++ {
		lines = append(lines, `{"level":"info","time":1791547356,"message":"line `+strings.Repeat("x", 80)+`"}`)
	}
	lines = append(lines, `{"level":"info","time":1791547356,"message":"the last one"}`)
	got, err := tailLog(writeLog(t, lines...))
	require.NoError(t, err)
	require.Less(t, len(got), 5000)
	require.Equal(t, "the last one", got[len(got)-1].Message)
	for _, l := range got {
		require.Equal(t, "info", l.Level, "every line read is whole")
	}

	_, err = tailLog(filepath.Join(t.TempDir(), "missing.log"))
	require.Error(t, err)
}

func TestServersPage(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", routesConf(), "", nil)
	require.NoError(t, err)
	w := get(r, "/servers")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "No folders are shared")
	require.NotContains(t, w.Body.String(), "<input type=\"folder\"", "the folder isn't an input with nothing to save it")
}

func TestDashboard(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", routesConf(), "", nil)
	require.NoError(t, err)

	w := get(r, "/")
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `<svg viewBox="0 0 300 100"`, "the chart is drawn on the server")
	require.Contains(t, body, "Not in use", "no cache configured")
	for _, gone := range []string{"jquery", "handlebars", "toastr", "bootstrap", "common.js", "Chart.min.js", "sleek", "materialdesignicons"} {
		require.NotContains(t, body, gone)
	}

	w = get(r, "/dashboard/stats")
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.HasPrefix(strings.TrimSpace(w.Body.String()), `<div id="stats"`))
}

// TestSpeedChart: the chart keeps one point per refresh, drops the history after a gap
// nobody watched, and scales to a round figure.
func TestSpeedChart(t *testing.T) {
	h := &speedHistory{}
	now := time.Now()
	for i := range 3 {
		h.add(speedSample{at: now.Add(time.Duration(i) * historyInterval), down: int64(i) * 1000})
	}
	got := h.add(speedSample{at: now.Add(2*historyInterval + 100*time.Millisecond), down: 3000})
	require.Len(t, got, 3, "a refresh sooner than the interval replaces the last point")
	require.Equal(t, int64(3000), got[2].down)

	got = h.add(speedSample{at: now.Add(time.Hour)})
	require.Len(t, got, 1, "an hour without anyone watching starts the line again")

	for i := range historyLen + 5 {
		got = h.add(speedSample{at: now.Add(2*time.Hour + time.Duration(i)*historyInterval)})
	}
	require.Len(t, got, historyLen)

	cv := newChartView([]speedSample{{down: 0}, {down: 1500 << 10, up: 300 << 10}})
	require.Equal(t, "2.0 MiB/s", cv.Ticks[0].Label)
	require.Equal(t, "1.0 MiB/s", cv.Ticks[1].Label)
	require.NotEmpty(t, cv.DownLine)
	require.Contains(t, cv.Label, "1.5 MiB/s down")

	require.Empty(t, newChartView(nil).DownLine, "one point isn't a line")
	require.Equal(t, int64(1024), niceRate(0))
	require.Equal(t, int64(5<<20), niceRate(3<<20))
}
