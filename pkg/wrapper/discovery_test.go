package wrapper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/httpclient"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/hashicorp/go-version"
)

// Synthetic public protocol shapes from Ronnie32/ogame.mod b7f9e90,
// 33d070c and a0c8113. No account captures, credentials or live game requests.
type discoveryFixture struct {
	version           string
	galaxy            string
	response          string
	status            int
	location          string
	before            string
	after             []string
	loseFirstResponse bool
}

type discoveryRequests struct {
	sends          atomic.Int32
	movements      atomic.Int32
	afterMovements atomic.Int32
}

func newDiscoveryTestBot(t *testing.T, fixture discoveryFixture) (*OGame, *discoveryRequests) {
	t.Helper()
	requests := &discoveryRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		switch {
		case req.Method == http.MethodPost && query.Get("component") == "galaxy":
			if query.Get("cp") != "1" {
				t.Errorf("galaxy origin cp = %q, want 1", query.Get("cp"))
			}
			if fixture.galaxy != "" {
				fmt.Fprint(w, fixture.galaxy)
			} else {
				fmt.Fprint(w, `{"system":{"galaxyContent":[{"position":2,"availableMissions":[{"missionType":18,"canSend":true}]}]},"token":"fresh-page-token","newAjaxToken":"different-cache-token"}`)
			}
		case req.Method == http.MethodGet && query.Get("component") == "movement":
			requests.movements.Add(1)
			fleets := fixture.before
			if requests.sends.Load() > 0 {
				index := int(requests.afterMovements.Add(1)) - 1
				if len(fixture.after) > 0 {
					if index >= len(fixture.after) {
						index = len(fixture.after) - 1
					}
					fleets = fixture.after[index]
				}
			}
			fmt.Fprint(w, discoveryMovementPage(fleets))
		case req.Method == http.MethodPost && query.Get("action") == "sendDiscoveryFleet":
			count := requests.sends.Add(1)
			if err := req.ParseForm(); err != nil {
				t.Errorf("parse dispatch: %v", err)
			}
			if query.Get("cp") != "1" || req.PostForm.Get("token") != "fresh-page-token" ||
				req.PostForm.Get("galaxy") != "1" || req.PostForm.Get("system") != "1" || req.PostForm.Get("position") != "2" {
				t.Errorf("wrong dispatch query/payload: %v %v", query, req.PostForm)
			}
			if fixture.loseFirstResponse && count == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("lose response: %v", err)
					return
				}
				_ = conn.Close() // the action was consumed, but its response was lost
				return
			}
			if fixture.location != "" {
				w.Header().Set("Location", fixture.location)
			}
			if fixture.status != 0 {
				w.WriteHeader(fixture.status)
			}
			if fixture.response == "" {
				fmt.Fprint(w, `{"response":{"success":true},"components":[],"newAjaxToken":"after-send"}`)
			} else {
				fmt.Fprint(w, fixture.response)
			}
		default:
			t.Errorf("unexpected game request: %s %s", req.Method, req.URL)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	serverURL, _ := url.Parse(server.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{Timeout: 2 * time.Second, Transport: fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != serverURL.Host || req.URL.Scheme != "http" {
			return nil, fmt.Errorf("fixture blocked non-local request: %s", req.URL)
		}
		return transport.RoundTrip(req)
	})}})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	ver := fixture.version
	if ver == "" {
		ver = "13.0.0"
	}
	bot.isLoggedInAtom.Store(true)
	bot.cache.serverURL = server.URL
	bot.cache.serverData.Version = ver
	bot.cache.token = "stale-cache-token"
	bot.extractor = getExtractorFor(version.Must(version.NewVersion(ver)))
	bot.extractor.SetLocation(time.UTC)
	bot.cache.planets.Store([]Planet{{Planet: ogame.Planet{ID: 1, Coordinate: ogame.NewPlanetCoordinate(1, 1, 1)}}})
	return bot, requests
}

func discoveryMovementPage(fleets string) string {
	return fmt.Sprintf(`<html><head><meta name="ogame-session" content="synthetic"/></head><body id="movement"><script>var currentPage = "movement"; var token = "movement-token";</script>
<span class="fleetSlots"><span class="current">%d</span><span class="all">10</span></span>
<span class="expSlots"><span class="current">0</span><span class="all">2</span></span>%s</body></html>`, strings.Count(fleets, `<div class="fleetDetails"`), fleets)
}

func discoveryFleetHTML(id int, mission int, returning bool, origin, destination string, destinationFigure string) string {
	return fmt.Sprintf(`<div class="fleetDetails" data-mission-type="%d" data-return-flight="%t" data-arrival-time="2000000000">
<a class="openCloseDetails" data-mission-id="%d" data-end-time="2000000000"></a>
<span class="originCoords"><a>[%s]</a></span><span class="originPlanet"><figure class="planet"></figure></span>
<span class="destinationCoords"><a>[%s]</a></span><span class="destinationPlanet"><figure class="%s"></figure></span>
<table class="fleetinfo"><tr><td>Metal:</td><td>0</td></tr><tr><td>Crystal:</td><td>0</td></tr><tr><td>Deuterium:</td><td>0</td></tr></table></div>`, mission, returning, id, origin, destination, destinationFigure)
}

func TestDiscoveryUsesFreshPageTokenAndSendingOrigin(t *testing.T) {
	for _, ver := range []string{"12.0.0", "13.0.0"} {
		t.Run(ver, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{version: ver})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err != nil {
				t.Fatal(err)
			}
			if requests.sends.Load() != 1 {
				t.Fatalf("dispatch POST count = %d, want 1", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryRequiresExplicitAvailableLifeformMission(t *testing.T) {
	for _, tt := range []struct{ name, content string }{
		{"missing target", `[]`},
		{"different target", `[{"position":3,"availableMissions":[{"missionType":18,"canSend":true}]}]`},
		{"missing mission", `[{"position":2,"availableMissions":[]}]`},
		{"other mission", `[{"position":2,"availableMissions":[{"missionType":15,"canSend":true}]}]`},
		{"false", `[{"position":2,"availableMissions":[{"missionType":18,"canSend":false}]}]`},
		{"server refusal", `[{"position":2,"availableMissions":[{"missionType":18,"canSend":"Cooldown"}]}]`},
		{"null", `[{"position":2,"availableMissions":[{"missionType":18,"canSend":null}]}]`},
		{"absent", `[{"position":2,"availableMissions":[{"missionType":18}]}]`},
		{"numeric", `[{"position":2,"availableMissions":[{"missionType":18,"canSend":1}]}]`},
		{"object", `[{"position":2,"availableMissions":[{"missionType":18,"canSend":{}}]}]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{galaxy: `{"system":{"galaxyContent":` + tt.content + `},"token":"fresh-page-token"}`})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil || err.Error() == "" {
				t.Errorf("expected informative eligibility error, got %v", err)
			}
			if requests.sends.Load() != 0 {
				t.Fatalf("ineligible target caused %d dispatches", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryRejectsMissingPageToken(t *testing.T) {
	for _, ver := range []string{"12.0.0", "13.0.0"} {
		t.Run(ver, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{version: ver, galaxy: `{"system":{"galaxyContent":[{"position":2,"availableMissions":[{"missionType":18,"canSend":true}]}]},"token":" "}`})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil || !strings.Contains(err.Error(), "token") {
				t.Errorf("expected token error, got %v", err)
			}
			if requests.sends.Load() != 0 {
				t.Fatalf("missing token caused %d dispatches", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryResponseValidation(t *testing.T) {
	for _, tt := range []struct {
		name, body, wantError string
	}{
		{"nested success", `{"response":{"success":true}}`, ""},
		{"top level success", `{"status":"success","success":true}`, ""},
		{"nested rejection", `{"response":{"success":false,"message":"Cooldown"}}`, "Cooldown"},
		{"top level rejection", `{"status":"failure","success":false,"message":"No slots"}`, "No slots"},
		{"top overrides nested", `{"success":false,"message":"Rejected","response":{"success":true}}`, "Rejected"},
		{"nested overrides top", `{"success":true,"response":{"success":false,"message":"Rejected"}}`, "Rejected"},
		{"error overrides success", `{"success":true,"errors":[{"error":999,"message":"Denied"}]}`, "Denied"},
		{"nested errors override success", `{"response":{"success":true,"errors":[{"error":999,"message":"Denied"}]}}`, "Denied"},
		{"error object overrides nested", `{"error":{"error":999,"message":"Denied"},"response":{"success":true}}`, "Denied"},
		{"boolean error overrides success", `{"success":true,"error":true,"message":"Denied"}`, "Denied"},
		{"failure status overrides nested", `{"status":"failure","message":"Denied","response":{"success":true}}`, "Denied"},
		{"null status", `{"status":null,"success":true}`, "discovery"},
		{"extra response layer", `{"response":{"success":true,"response":{"success":false}}}`, "discovery"},
		{"busy", `{"status":"failure","success":false,"errors":[{"message":"busy","error":100}],"error":{"message":"busy","error":100}}`, "busy"},
		{"nested busy", `{"response":{"success":false,"errors":[{"message":"busy","error":100}]}}`, "busy"},
		{"pending", `{"status":"pending","success":true}`, "discovery"},
		{"empty", `{}`, "discovery"},
		{"null", `null`, "discovery"},
		{"status only", `{"status":"success"}`, "discovery"},
		{"missing nested success", `{"response":{"message":"unknown"}}`, "discovery"},
		{"null success", `{"response":{"success":null}}`, "discovery"},
		{"string success", `{"response":{"success":"true"}}`, "discovery"},
		{"array", `[]`, "discovery"},
		{"malformed", `{"response":`, "discovery"},
		{"HTML", `<html>unknown</html>`, "discovery"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{response: tt.body})
			err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor)
			if tt.wantError == "" && err != nil {
				t.Errorf("expected accepted discovery, got %v", err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Errorf("error = %v, want containing %q", err, tt.wantError)
			}
			if requests.sends.Load() != 1 {
				t.Errorf("dispatches = %d, want 1", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryInternal302RequiresUniqueNewMatchingFleet(t *testing.T) {
	old := discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet")
	newFleet := discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")
	for _, tt := range []struct {
		name, before string
		after        []string
		wantSuccess  bool
	}{
		{"new fleet", old, []string{old + newFleet}, true},
		{"delayed new fleet", old, []string{old, old + newFleet}, true},
		{"old fleet only", old, []string{old}, false},
		{"no fleet", "", []string{""}, false},
		{"wrong mission", "", []string{discoveryFleetHTML(41, 3, false, "1:1:1", "1:1:2", "planet")}, false},
		{"returning", "", []string{discoveryFleetHTML(41, 18, true, "1:1:1", "1:1:2", "planet")}, false},
		{"wrong origin", "", []string{discoveryFleetHTML(41, 18, false, "1:1:3", "1:1:2", "planet")}, false},
		{"wrong destination", "", []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:3", "planet")}, false},
		{"wrong celestial type", "", []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "moon")}, false},
		{"missing ID", "", []string{discoveryFleetHTML(0, 18, false, "1:1:1", "1:1:2", "planet")}, false},
		{"existing ID on changed route", discoveryFleetHTML(41, 3, false, "1:1:1", "1:1:3", "planet"), []string{newFleet}, false},
		{"ambiguous new fleets", "", []string{newFleet + discoveryFleetHTML(42, 18, false, "1:1:1", "1:1:2", "planet")}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview&displayError=1", before: tt.before, after: tt.after, response: `redirect`})
			err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor)
			if (err == nil) != tt.wantSuccess {
				t.Errorf("error = %v, want success %t", err, tt.wantSuccess)
			}
			if requests.sends.Load() != 1 {
				t.Errorf("dispatches = %d, want 1", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryRejectsUntrustedRedirectEvenWhenNewFleetAppears(t *testing.T) {
	for _, location := range []string{"https://external.invalid/game/index.php?page=ingame", "/game/index.php?page=login", "/game/index.php?page=lobbylogin", "/game/index.php?page=logout", "/login", "//external.invalid/game/index.php", "?page=unknown", ""} {
		t.Run(location, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: location, after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
				t.Error("accepted untrusted redirect")
			}
			if requests.sends.Load() != 1 || requests.afterMovements.Load() != 0 {
				t.Errorf("dispatches/reconciliation = %d/%d, want 1/0", requests.sends.Load(), requests.afterMovements.Load())
			}
		})
	}
}

func TestDiscoveryDoesNotReplayLostResponse(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{loseFirstResponse: true, after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipInterceptor); err == nil {
		t.Error("lost response must report an ambiguous dispatch error")
	}
	if requests.sends.Load() != 1 {
		t.Fatalf("lost response caused %d dispatches, want 1", requests.sends.Load())
	}
}

func TestDiscoveryRejectsFailedLegacyPreflight(t *testing.T) {
	for _, failure := range []string{`"success":false`, `"status":"failure"`, `"error":true`} {
		t.Run(failure, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{version: "12.0.0", galaxy: `{` + failure + `,"system":{"galaxyContent":[{"position":2,"availableMissions":[{"missionType":18,"canSend":true}]}]},"token":"fresh-page-token"}`})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
				t.Error("accepted failed preflight")
			}
			if requests.sends.Load() != 0 {
				t.Errorf("failed preflight caused %d dispatches", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryRequiresUsableFleetWatermark(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{before: discoveryFleetHTML(0, 18, false, "1:1:1", "1:1:2", "planet")})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
		t.Error("accepted watermark with unknown fleet ID")
	}
	if requests.sends.Load() != 0 {
		t.Errorf("unknown fleet ID caused %d dispatches", requests.sends.Load())
	}
}

func TestDiscoveryRejectionCannotBeOverriddenByNewFleet(t *testing.T) {
	for _, body := range []string{`{"response":{"success":false,"message":"Cooldown"}}`, `{"status":"failure","success":false,"errors":[{"message":"busy","error":100}]}`, `{"status":"pending","success":true}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{response: body, after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
				t.Error("new fleet overrode a rejected or malformed action response")
			}
			if requests.sends.Load() != 1 || requests.afterMovements.Load() != 0 {
				t.Errorf("dispatches/reconciliation = %d/%d, want 1/0", requests.sends.Load(), requests.afterMovements.Load())
			}
		})
	}
}

func TestDiscoveryRejectsErrorHTTPStatusWithSuccessBody(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: status})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil || err.Error() == "" {
				t.Errorf("expected informative HTTP error, got %v", err)
			}
			if requests.sends.Load() != 1 {
				t.Errorf("dispatches = %d, want 1", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryAvailablePositionsRequireLifeformMission(t *testing.T) {
	bot, _ := newDiscoveryTestBot(t, discoveryFixture{galaxy: `{"system":{"galaxyContent":[{"position":2,"availableMissions":[{"missionType":15,"canSend":true}]},{"position":3,"availableMissions":[{"missionType":18,"canSend":false}]},{"position":4,"availableMissions":[{"missionType":18,"canSend":true},{"missionType":18,"canSend":true}]}]},"token":"fresh-page-token"}`})
	got, err := bot.getPositionsAvailableForDiscoveryFleet(1, 1, ChangePlanet(1), SkipRetry, SkipInterceptor)
	if err != nil || len(got) != 1 || !got[0].Equal(ogame.NewPlanetCoordinate(1, 1, 4)) {
		t.Fatalf("available positions = %v, error = %v; want [1:1:4]", got, err)
	}
}

func TestDiscoveryWithFleetReturnsOnlyNewMatchingFleet(t *testing.T) {
	old := discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet")
	for _, tt := range []struct {
		name, after string
		wantID      ogame.FleetID
	}{
		{"new fleet", old + discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet"), 41},
		{"old fleet only", old, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{before: old, after: []string{tt.after}})
			fleet, err := bot.sendDiscoveryFleet2(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor)
			if tt.wantID > 0 && (err != nil || fleet.ID != tt.wantID) {
				t.Errorf("fleet ID = %d, error = %v; want %d", fleet.ID, err, tt.wantID)
			}
			if tt.wantID == 0 && err == nil {
				t.Errorf("old fleet %d was returned as newly sent", fleet.ID)
			}
			if requests.sends.Load() != 1 {
				t.Errorf("dispatches = %d, want 1", requests.sends.Load())
			}
		})
	}
}
