package wrapper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/httpclient"
	"github.com/labstack/echo/v4"
)

type highscoreNavigationTransport func(*http.Request) (*http.Response, error)

func (f highscoreNavigationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// All requests, including unexpected login attempts, are confined to this fake
// transport. No credentials, real player IDs or network connections are used.
func newHighscoreNavigationBot(t *testing.T, serverVersion string, check func(*http.Request)) *OGame {
	t.Helper()
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{Transport: highscoreNavigationTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "ogame.invalid" {
			return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
		}
		check(req)
		body := `<html><head><meta name="ogame-session" content="fixture"/></head><body><table id="ranks"><tr id="fixture-player-4242"><td>Fixture player</td></tr></table></body></html>`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	bot.isLoggedInAtom.Store(true)
	bot.cache.serverURL = "https://ogame.invalid"
	bot.cache.serverData.Version = serverVersion
	return bot
}

func TestHighscoreNavigation(t *testing.T) {
	for _, tc := range []struct {
		name, version, query, want string
	}{
		{"v13 galaxy link", "13.0.0", "page=highscore&searchRelId=4242", "page=ingame&component=highscore&searchRelId=4242"},
		{"revision and filters", "13.0.1-r4", "page=highscore&searchRelId=4242&category=1&type=3&site=2&cp=99&filter=a%26b&filter=c+d", "page=ingame&component=highscore&searchRelId=4242&category=1&type=3&site=2&cp=99&filter=a%26b&filter=c+d"},
		{"beta version", "13.1.0-beta17", "page=highscore&searchRelId=4242", "page=ingame&component=highscore&searchRelId=4242"},
		{"old server", "12.0.0", "page=highscore&searchRelId=4242", "page=highscore&searchRelId=4242"},
		{"unknown server", "", "page=highscore&searchRelId=4242", "page=highscore&searchRelId=4242"},
		{"invalid version", "unknown", "page=highscore&searchRelId=4242", "page=highscore&searchRelId=4242"},
		{"modern link", "13.0.0", "page=ingame&component=highscore&searchRelId=4242", "page=ingame&component=highscore&searchRelId=4242"},
		{"explicit component", "13.0.0", "page=highscore&component=other&searchRelId=4242", "page=highscore&component=other&searchRelId=4242"},
		{"fleet unchanged", "13.0.0", "page=ingame&component=fleetdispatch", "page=ingame&component=fleetdispatch"},
		{"ajax unchanged", "13.0.0", "page=highscoreContent&ajax=1", "page=highscoreContent&ajax=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vals, _ := url.ParseQuery(tc.query)
			before := vals.Encode()
			want, _ := url.ParseQuery(tc.want)
			calls := 0
			bot := newHighscoreNavigationBot(t, tc.version, func(req *http.Request) {
				calls++
				if req.Method != http.MethodGet || !reflect.DeepEqual(req.URL.Query(), want) {
					t.Errorf("request = %s %s, want GET %s", req.Method, req.URL, want.Encode())
				}
			})
			body, err := bot.GetPageContent(vals)
			if err != nil || !strings.Contains(string(body), "fixture-player-4242") {
				t.Fatalf("body=%q err=%v", body, err)
			}
			if calls != 1 || vals.Encode() != before {
				t.Fatalf("requests=%d (want 1), caller query changed=%v", calls, vals.Encode() != before)
			}
		})
	}
}

func TestHighscoreNavigationManualTransaction(t *testing.T) {
	bot := newHighscoreNavigationBot(t, "13.0.1-r4", func(req *http.Request) {
		if req.URL.Query().Get("page") != "ingame" || req.URL.Query().Get("component") != "highscore" || req.URL.Query().Get("searchRelId") != "4242" {
			t.Errorf("manual-mode request lost route/focus: %s", req.URL)
		}
	})
	tx := bot.BeginNamed("manual mode")
	defer tx.Done()
	if _, err := tx.GetPageContent(url.Values{"page": {"highscore"}, "searchRelId": {"4242"}}); err != nil {
		t.Fatal(err)
	}
}

func TestHighscoreNavigationBrowserProxy(t *testing.T) {
	bot := newHighscoreNavigationBot(t, "13.0.0", func(req *http.Request) {
		if req.URL.Query().Get("page") != "ingame" || req.URL.Query().Get("component") != "highscore" || req.URL.Query().Get("searchRelId") != "4242" {
			t.Errorf("proxy request lost route/focus: %s", req.URL)
		}
	})
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/game/index.php?page=highscore&searchRelId=4242", nil), rec)
	c.Set("bot", bot)
	if err := GetFromGameHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "fixture-player-4242") {
		t.Fatalf("proxy response: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHighscoreNavigationDoesNotRewritePost(t *testing.T) {
	bot := newHighscoreNavigationBot(t, "13.0.0", func(req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Query().Get("page") != "highscore" || req.URL.Query().Has("component") {
			t.Errorf("POST was rewritten: %s %s", req.Method, req.URL)
		}
	})
	if _, err := bot.PostPageContent(url.Values{"page": {"highscore"}}, url.Values{"searchRelId": {"4242"}}); err != nil {
		t.Fatal(err)
	}
}
