package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/Apollogeddon/distribyted/internal/config"
	"github.com/Apollogeddon/distribyted/internal/torrent"
)

// The Routes page is rendered on the server: the page and the fragments htmx swaps into it
// come from the same templates, routes.html and routes_table.html.

const (
	// seederGrace withholds the few-seeders warning while a new torrent is still connecting:
	// Seeders counts only confirmed, connected seeders, which takes a while to ramp up.
	seederGrace = 20 * time.Second
	// largePiece is the piece size above which a read waits noticeably for its first piece.
	largePiece = 4 << 20
)

type routesPage struct {
	Title  string
	Routes []routeView
	// Names is every route a torrent can be added to: the configured ones and any that
	// only exist because a torrent was added to them.
	Names []string
}

type routeView struct {
	Name     string
	Torrents []torrentView
}

type torrentView struct {
	// ID keeps each row's element the same from one refresh to the next.
	ID, DeleteURL     string
	Route, Name, Hash string
	Down, Up          string
	Peers, Seeders    int
	PieceSize         string
	// Pieces is empty until the torrent's metadata arrives.
	Pieces   []pieceSegment
	Progress string
	Health   health
}

type pieceSegment struct {
	Class string
	Width string
	Label string
}

// health is a torrent's state as one word and, when it isn't fine, the reason, which the
// page shows next to it rather than only in a tooltip.
type health struct {
	Level  string // ok, wait or warn
	Label  string
	Reason string
}

var pieceClasses = map[torrent.PieceStatus][2]string{
	torrent.Checking: {"piece-checking", "checking"},
	torrent.Partial:  {"piece-partial", "partly downloaded"},
	torrent.Complete: {"piece-complete", "downloaded"},
	torrent.Waiting:  {"piece-waiting", "not downloaded"},
	torrent.Error:    {"piece-error", "failed"},
}

func newRoutesPage(conf *config.Root, ss *torrent.Stats) routesPage {
	byName := map[string]*routeView{}
	var names []string
	add := func(name string) *routeView {
		if rv, ok := byName[name]; ok {
			return rv
		}
		rv := &routeView{Name: name}
		byName[name] = rv
		names = append(names, name)
		return rv
	}

	if conf != nil {
		for _, r := range conf.Routes {
			add(r.Name)
		}
	}
	if ss != nil {
		for _, rs := range ss.RoutesStats() {
			rv := add(rs.Name)
			for _, ts := range rs.TorrentStats {
				rv.Torrents = append(rv.Torrents, newTorrentView(rs.Name, ts))
			}
		}
	}

	sort.Strings(names)
	page := routesPage{Title: "Routes", Names: names}
	for i, n := range names {
		rv := *byName[n]
		for j := range rv.Torrents {
			rv.Torrents[j].ID = fmt.Sprintf("torrent-%d-%s", i, rv.Torrents[j].Hash)
		}
		page.Routes = append(page.Routes, rv)
	}
	return page
}

// Form is the empty add-magnet form the page starts with.
func (p routesPage) Form() addMagnetForm {
	return addMagnetForm{Names: p.Names}
}

func newTorrentView(route string, ts *torrent.TorrentStats) torrentView {
	tv := torrentView{
		DeleteURL: "/routes/" + url.PathEscape(route) + "/torrents/" + url.PathEscape(ts.Hash),
		Route:     route,
		Name:      ts.Name,
		Hash:      ts.Hash,
		Down:      rate(ts.DownloadedBytes, ts.TimePassed),
		Up:        rate(ts.UploadedBytes, ts.TimePassed),
		Peers:     ts.Peers,
		Seeders:   ts.Seeders,
		PieceSize: humanize.IBytes(uint64(max(ts.PieceSize, 0))),
	}

	if ts.TotalPieces == 0 || len(ts.PieceChunks) == 0 {
		tv.Health = health{Level: "wait", Label: "Fetching metadata", Reason: "Waiting for a peer to send the torrent's file list."}
		return tv
	}

	complete, start := 0, 0
	for _, c := range ts.PieceChunks {
		cls, ok := pieceClasses[c.Status]
		if !ok {
			cls = [2]string{"piece-waiting", string(c.Status)}
		}
		if c.Status == torrent.Complete {
			complete += c.NumPieces
		}
		tv.Pieces = append(tv.Pieces, pieceSegment{
			Class: cls[0],
			Width: fmt.Sprintf("%.4f%%", float64(c.NumPieces)*100/float64(ts.TotalPieces)),
			Label: fmt.Sprintf("Pieces %d–%d %s", start, start+c.NumPieces-1, cls[1]),
		})
		start += c.NumPieces
	}
	tv.Progress = fmt.Sprintf("%d%% · %s of %s pieces", complete*100/ts.TotalPieces,
		humanize.Comma(int64(complete)), humanize.Comma(int64(ts.TotalPieces)))

	tv.Health = torrentHealth(ts)
	return tv
}

func torrentHealth(ts *torrent.TorrentStats) health {
	var reasons []string
	if ts.PieceSize > largePiece {
		reasons = append(reasons, fmt.Sprintf("Pieces are %s, so the first read of each part of a file waits longer. 1 MiB or less starts faster.", humanize.IBytes(uint64(ts.PieceSize))))
	}
	settled := time.Duration(ts.AgeSeconds*float64(time.Second)) >= seederGrace
	switch {
	case settled && ts.Peers == 0:
		reasons = append(reasons, "No peers are connected, so nothing more can be downloaded.")
	case settled && ts.Seeders < 2:
		reasons = append(reasons, fmt.Sprintf("Only %d seeder%s connected, so reads may be slow.", ts.Seeders, plural(ts.Seeders)))
	}
	if len(reasons) == 0 {
		if !settled && ts.Seeders < 2 {
			return health{Level: "wait", Label: "Connecting", Reason: "Finding peers for this torrent."}
		}
		return health{Level: "ok", Label: "Healthy"}
	}
	return health{Level: "warn", Label: "Needs attention", Reason: strings.Join(reasons, " ")}
}

func rate(bytes int64, seconds float64) string {
	if seconds <= 0 || bytes <= 0 {
		return "0 B/s"
	}
	return humanize.IBytes(uint64(float64(bytes)/seconds)) + "/s"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

var routesHandler = func(conf *config.Root, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, routesView(newRoutesPage(conf, ss)))
	}
}

// routesTableHandler renders just the routes, which the page polls.
var routesTableHandler = func(conf *config.Root, ss *torrent.Stats) gin.HandlerFunc {
	return func(c *gin.Context) {
		render(c, http.StatusOK, routesTable(newRoutesPage(conf, ss)))
	}
}

type addMagnetForm struct {
	Names  []string
	Route  string
	Magnet string
	Error  string
}

// routesAddHandler adds a magnet from the page's form. A problem re-renders the form with
// the message under the field (422, which the page's htmx config swaps in); success
// returns an empty form and tells the page to close the dialog and refresh the routes.
var routesAddHandler = func(conf *config.Root, ss *torrent.Stats, s torrentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := addMagnetForm{
			Names:  newRoutesPage(conf, ss).Names,
			Route:  c.PostForm("route"),
			Magnet: strings.TrimSpace(c.PostForm("magnet")),
		}
		fail := func(msg string) {
			form.Error = msg
			render(c, http.StatusUnprocessableEntity, magnetFormView(form))
		}

		switch {
		case !strings.HasPrefix(form.Magnet, "magnet:?"):
			fail("Paste a magnet link: it starts with magnet:?")
			return
		case config.ValidateRouteName(form.Route) != nil:
			fail("Choose a route.")
			return
		}
		if err := s.AddMagnet(form.Route, form.Magnet); err != nil {
			fail("Couldn't add the magnet: " + err.Error())
			return
		}

		triggerEvents(c, map[string]any{
			"routes-changed": true,
			"close-dialog":   "add-magnet",
			"toast":          toast{Level: "success", Message: "Magnet added to " + form.Route + "."},
		})
		render(c, http.StatusOK, magnetFormView(addMagnetForm{Names: form.Names, Route: form.Route}))
	}
}

var routesDeleteHandler = func(s torrentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		route, hash := c.Param("route"), strings.ToLower(c.Param("hash"))
		if err := s.RemoveFromHash(route, hash); err != nil {
			c.String(http.StatusInternalServerError, "Couldn't delete the torrent: %s", err)
			return
		}
		triggerEvents(c, map[string]any{
			"routes-changed": true,
			"toast":          toast{Level: "success", Message: "Torrent deleted."},
		})
		c.Status(http.StatusOK)
	}
}

type toast struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// triggerEvents has htmx fire these events on the page once it has the response.
func triggerEvents(c *gin.Context, events map[string]any) {
	if b, err := jsonString(events); err == nil {
		c.Header("HX-Trigger", b)
	}
}

// jsonString encodes v for a response header. Browsers read header bytes as Latin-1, so
// everything past ASCII is written as a \u escape, which JSON.parse turns back into the
// character: a “ or an é in a name would otherwise arrive garbled.
func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, r := range string(b) {
		switch {
		case r < utf8.RuneSelf:
			sb.WriteRune(r)
		case r > 0xffff:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&sb, "\\u%04x\\u%04x", r1, r2)
		default:
			fmt.Fprintf(&sb, "\\u%04x", r)
		}
	}
	return sb.String(), nil
}

// countOf is "1 torrent", "3 torrents".
func countOf(n int, word string) string {
	return fmt.Sprintf("%d %s%s", n, word, plural(n))
}
