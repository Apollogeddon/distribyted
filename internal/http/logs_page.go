package http

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The Logs page shows the newest lines of distribyted's JSON log, newest first, and checks
// for new ones every few seconds. A level filter is part of the URL.

const (
	logTailBytes = 256 << 10
	logMaxLines  = 500
)

var logLevels = []string{"all", "error", "warn", "info", "debug"}

type logsPage struct {
	Title string
	Level string
	// Query keeps the lines holding every word typed, in the message, component or fields.
	Query   string
	Filters []logFilter
	ListURL string
	Lines   []logLine
	Error   string
}

type logFilter struct {
	Level, Label, Href, ListURL string
	Active                      bool
}

type logLine struct {
	Time, FullTime   string
	Level, Component string
	Message          string
	Fields           []logField
}

type logField struct{ Key, Value string }

func newLogsPage(logPath, level, query string) logsPage {
	if level == "" || !contains(logLevels, level) {
		level = "all"
	}
	query = strings.TrimSpace(query)
	params := func(l string) string {
		v := url.Values{"level": {l}}
		if query != "" {
			v.Set("q", query)
		}
		return v.Encode()
	}
	page := logsPage{Title: "Logs", Level: level, Query: query, ListURL: "/logs/list?" + params(level)}
	for _, l := range logLevels {
		label := map[string]string{"all": "All", "error": "Errors", "warn": "Warnings", "info": "Info", "debug": "Debug"}[l]
		page.Filters = append(page.Filters, logFilter{
			Level: l, Label: label, Active: l == level,
			Href: "/logs?" + params(l), ListURL: "/logs/list?" + params(l),
		})
	}
	words := strings.Fields(strings.ToLower(query))

	lines, err := tailLog(logPath)
	if err != nil {
		page.Error = "Couldn't read the log file: " + err.Error()
		return page
	}
	for i := len(lines) - 1; i >= 0 && len(page.Lines) < logMaxLines; i-- {
		l := lines[i]
		if level != "all" && l.Level != level || !matchesWords(l.searchText(), words) {
			continue
		}
		page.Lines = append(page.Lines, l)
	}
	return page
}

// tailLog reads the last logTailBytes of the log and parses each whole line.
func tailLog(p string) ([]logLine, error) {
	if p == "" {
		return nil, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(fi.Size()-logTailBytes, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		// the first line is cut off
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}

	var out []logLine
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), logTailBytes)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		out = append(out, parseLogLine(sc.Bytes()))
	}
	return out, sc.Err()
}

func parseLogLine(b []byte) logLine {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return logLine{Message: string(b)}
	}
	l := logLine{}
	l.Level, _ = m["level"].(string)
	l.Component, _ = m["component"].(string)
	l.Message, _ = m["message"].(string)
	if ts, ok := m["time"].(float64); ok {
		t := time.Unix(int64(ts), 0)
		l.Time = t.Format("Jan 2 15:04:05")
		l.FullTime = t.Format(time.RFC1123)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		switch k {
		case "level", "component", "message", "time":
		default:
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := m[k].(string)
		if !ok {
			j, _ := json.Marshal(m[k])
			v = string(j)
		}
		l.Fields = append(l.Fields, logField{Key: k, Value: v})
	}
	return l
}

// searchText is what a search looks in.
func (l logLine) searchText() string {
	var b strings.Builder
	b.WriteString(l.Message + " " + l.Component)
	for _, f := range l.Fields {
		b.WriteString(" " + f.Key + "=" + f.Value)
	}
	return b.String()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var logsHandler = func(logPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, logsView(newLogsPage(logPath, c.Query("level"), c.Query("q"))))
	}
}

var logsListHandler = func(logPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := newLogsPage(logPath, c.Query("level"), c.Query("q"))
		// the search box asks with push set, and gets the page's address for the address bar
		if c.Query("push") != "" {
			c.Header("HX-Push-Url", strings.Replace(page.ListURL, "/logs/list", "/logs", 1))
		}
		render(c, http.StatusOK, logsList(page))
	}
}
