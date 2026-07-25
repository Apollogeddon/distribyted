package http

import (
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Apollogeddon/distribyted/internal/config"
	dtorrent "github.com/Apollogeddon/distribyted/internal/torrent"
	"github.com/stretchr/testify/require"
)

func authedConf() *config.Root {
	return &config.Root{
		HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, User: "test", Pass: "test"},
	}
}

func loginRequest(user, pass string) *http.Request {
	form := url.Values{"username": {user}, "password": {pass}}
	req, _ := http.NewRequest("POST", "/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestQBitLogin_Success(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, loginRequest("test", "test"))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Ok.", w.Body.String())

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, sessionCookieName, cookies[0].Name)
	require.True(t, cookies[0].HttpOnly)
	require.NotEmpty(t, cookies[0].Value)
}

func TestQBitLogin_Failure(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, loginRequest("test", "wrong"))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "Fails.", w.Body.String())
	require.Empty(t, w.Result().Cookies())
}

func TestQBitProtected_NoSessionIs403(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	endpoints := []struct {
		method, path string
	}{
		{"GET", "/api/v2/torrents/info"},
		{"GET", "/api/v2/sync/maindata"},
		{"POST", "/api/v2/torrents/add"},
		{"GET", "/api/v2/app/webapiVersion"},
	}

	for _, e := range endpoints {
		t.Run(e.path, func(t *testing.T) {
			req, _ := http.NewRequest(e.method, e.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestQBitArrCookieFlow(t *testing.T) {
	mockSvc := &mockTorrentService{
		addMagnetFunc: func(route, magnet string) error {
			require.Equal(t, "torrents", route)
			require.Equal(t, "test-magnet", magnet)
			return nil
		},
	}

	r, err := NewHandler(nil, dtorrent.NewStats(), mockSvc, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	form := url.Values{"username": {"test"}, "password": {"test"}}
	resp, err := client.PostForm(srv.URL+"/api/v2/auth/login", form)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, "Ok.", string(body))

	addResp, err := client.PostForm(srv.URL+"/api/v2/torrents/add", url.Values{"urls": {"test-magnet"}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, addResp.StatusCode)
	addResp.Body.Close()

	infoResp, err := client.Get(srv.URL + "/api/v2/torrents/info")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, infoResp.StatusCode)
	infoResp.Body.Close()
}

func TestQBitLogout(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}

	form := url.Values{"username": {"test"}, "password": {"test"}}
	resp, err := client.PostForm(srv.URL+"/api/v2/auth/login", form)
	require.NoError(t, err)
	resp.Body.Close()

	logoutResp, err := client.PostForm(srv.URL+"/api/v2/auth/logout", url.Values{})
	require.NoError(t, err)
	logoutResp.Body.Close()

	infoResp, err := client.Get(srv.URL + "/api/v2/torrents/info")
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, infoResp.StatusCode)
	infoResp.Body.Close()
}

func TestLoginPage_Get(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `name="username"`)
	require.Contains(t, w.Body.String(), `name="password"`)
}

func TestLogin_Success(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	form := url.Values{"username": {"test"}, "password": {"test"}, "next": {"/routes"}}
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "/routes", w.Header().Get("Location"))
	require.NotEmpty(t, w.Result().Cookies())
}

func TestLogin_Failure(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	form := url.Values{"username": {"test"}, "password": {"wrong"}}
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/login?error=1")
	require.Empty(t, w.Result().Cookies())
}

func TestLogout(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

	loginResp, err := client.PostForm(srv.URL+"/login", url.Values{"username": {"test"}, "password": {"test"}})
	require.NoError(t, err)
	loginResp.Body.Close()

	logoutResp, err := client.Get(srv.URL + "/logout")
	require.NoError(t, err)
	logoutResp.Body.Close()
	require.Equal(t, "/login", logoutResp.Header.Get("Location"))

	homeResp, err := client.Get(srv.URL + "/")
	require.NoError(t, err)
	homeResp.Body.Close()
	require.Equal(t, http.StatusFound, homeResp.StatusCode)
	require.Contains(t, homeResp.Header.Get("Location"), "/login")
}

func TestWebUI_RequiresAuth(t *testing.T) {
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", authedConf(), "")
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/login")

	// public assets stay public
	reqAssets, _ := http.NewRequest("GET", "/assets/js/common.js", nil)
	wAssets := httptest.NewRecorder()
	r.ServeHTTP(wAssets, reqAssets)
	require.NotEqual(t, http.StatusFound, wAssets.Code)

	// with a valid session
	sid, err := newSessionStoreForTest(t, r)
	require.NoError(t, err)
	reqOK, _ := http.NewRequest("GET", "/", nil)
	reqOK.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	wOK := httptest.NewRecorder()
	r.ServeHTTP(wOK, reqOK)
	require.Equal(t, http.StatusOK, wOK.Code)
}

func TestHTTPFS_RequiresAuth(t *testing.T) {
	conf := authedConf()
	conf.HTTPGlobal.HTTPFS = true

	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, http.Dir("."), "", conf, "")
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/fs/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/login")
}

func TestUnsetCredentialsFailClosed(t *testing.T) {
	conf := &config.Root{HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444}}
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "")
	require.NoError(t, err)

	reqAPI, _ := http.NewRequest("GET", "/api/status", nil)
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)
	require.Equal(t, http.StatusFound, wAPI.Code) // browser-style redirect, not open

	reqQbit, _ := http.NewRequest("GET", "/api/v2/torrents/info", nil)
	wQbit := httptest.NewRecorder()
	r.ServeHTTP(wQbit, reqQbit)
	require.Equal(t, http.StatusForbidden, wQbit.Code)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, loginRequest("", ""))
	require.Equal(t, "Fails.", w.Body.String())
}

func TestAuthDisabled_AllowsEverything(t *testing.T) {
	conf := &config.Root{HTTPGlobal: &config.HTTPGlobal{IP: "0.0.0.0", Port: 4444, DisableAuth: true}}
	r, err := NewHandler(nil, dtorrent.NewStats(), nil, nil, nil, nil, "", conf, "")
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	reqQbit, _ := http.NewRequest("GET", "/api/v2/torrents/info", nil)
	wQbit := httptest.NewRecorder()
	r.ServeHTTP(wQbit, reqQbit)
	require.Equal(t, http.StatusOK, wQbit.Code)
}

func TestSessionStore(t *testing.T) {
	st := newSessionStore(5 * time.Millisecond)

	sid, err := st.create()
	require.NoError(t, err)
	require.True(t, st.validate(sid))

	require.False(t, st.validate("unknown-sid"))

	st.destroy(sid)
	require.False(t, st.validate(sid))

	expiring, err := st.create()
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	require.False(t, st.validate(expiring))
}

// newSessionStoreForTest logs in against r and returns the resulting session ID.
func newSessionStoreForTest(t *testing.T, r http.Handler) (string, error) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, loginRequest("test", "test"))
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c.Value, nil
		}
	}
	return "", errors.New("no session cookie set")
}
