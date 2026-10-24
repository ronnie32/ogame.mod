package wrapper

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alaingilbert/ogame/pkg/ogame"
)

func TestDiscoveryReconciliationRequiresExplicitDirectionAndTypes(t *testing.T) {
	complete := discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")
	for _, tc := range []struct{ name, fleet string }{
		{"missing direction", strings.Replace(complete, ` data-return-flight="false"`, "", 1)},
		{"invalid direction", strings.Replace(complete, `data-return-flight="false"`, `data-return-flight="unknown"`, 1)},
		{"empty direction", strings.Replace(complete, `data-return-flight="false"`, `data-return-flight=""`, 1)},
		{"numeric direction", strings.Replace(complete, `data-return-flight="false"`, `data-return-flight="0"`, 1)},
		{"missing origin type", strings.Replace(complete, `<span class="originPlanet"><figure class="planet"></figure></span>`, "", 1)},
		{"missing destination type", strings.Replace(complete, `<span class="destinationPlanet"><figure class="planet"></figure></span>`, "", 1)},
		{"unknown origin type", strings.Replace(complete, `<span class="originPlanet"><figure class="planet">`, `<span class="originPlanet"><figure class="unknown">`, 1)},
		{"unknown destination type", strings.Replace(complete, `<span class="destinationPlanet"><figure class="planet">`, `<span class="destinationPlanet"><figure class="unknown">`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{tc.fleet}})
			err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor)
			if err == nil {
				t.Errorf("accepted incomplete reconciliation evidence: %s (POSTs=%d)", tc.name, requests.sends.Load())
			}
			if requests.sends.Load() != 1 {
				t.Errorf("dispatch POSTs = %d, want 1", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryMovementProofDoesNotFollowRedirect(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
	original := bot.device.GetClient().Transport
	var redirected atomic.Bool
	bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(req)
		if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" && !redirected.Swap(true) {
			resp.StatusCode = http.StatusFound
			resp.Status = "302 Found"
			resp.Header.Set("Location", req.URL.String())
		}
		return resp, err
	})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
		t.Error("followed a redirected movement response as valid initial proof")
	}
	if requests.sends.Load() != 0 || requests.movements.Load() != 1 {
		t.Errorf("dispatches/movement reads = %d/%d, want 0/1", requests.sends.Load(), requests.movements.Load())
	}
}

func TestDiscoveryMovementSnapshotRequiresCompleteUniqueIDs(t *testing.T) {
	old := discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet")
	for _, tc := range []struct {
		name, before, from, to string
	}{
		{"missing occupied count", "", `<span class="current">0</span>`, ""},
		{"invalid occupied count", "", `<span class="current">0</span>`, `<span class="current">unknown</span>`},
		{"negative occupied count", "", `<span class="current">0</span>`, `<span class="current">-1</span>`},
		{"occupied count below rows", old, `<span class="current">1</span>`, `<span class="current">0</span>`},
		{"duplicate fleet ID", old + old, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", before: tc.before, after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
			original := bot.device.GetClient().Transport
			bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
				resp, err := original.RoundTrip(req)
				if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" && requests.sends.Load() == 0 && tc.from != "" {
					body, readErr := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if readErr != nil {
						return nil, readErr
					}
					resp.Body = io.NopCloser(strings.NewReader(strings.Replace(string(body), tc.from, tc.to, 1)))
				}
				return resp, err
			})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
				t.Error("accepted incomplete or duplicate fleet watermark")
			}
			if requests.sends.Load() != 0 {
				t.Errorf("invalid watermark caused %d dispatches", requests.sends.Load())
			}
		})
	}
}

func TestDiscoveryMovementEvidenceRequiresSuccessfulHTTPBeforeAndAfterSend(t *testing.T) {
	for _, when := range []string{"before", "after"} {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s/%d", when, status), func(t *testing.T) {
				bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
				original := bot.device.GetClient().Transport
				bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
					resp, err := original.RoundTrip(req)
					if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" && (requests.sends.Load() == 0) == (when == "before") {
						resp.StatusCode = status
						resp.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
					}
					return resp, err
				})
				if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
					t.Error("accepted failed HTTP movement evidence")
				}
				wantPosts := int32(0)
				if when == "after" {
					wantPosts = 1
				}
				if requests.sends.Load() != wantPosts {
					t.Errorf("dispatch POSTs = %d, want %d", requests.sends.Load(), wantPosts)
				}
			})
		}
	}
}
func TestDiscoveryMovementReadRejectsHTTPError(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
	original := bot.device.GetClient().Transport
	bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(req)
		if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" {
			resp.StatusCode = http.StatusBadRequest
			resp.Status = "400 Bad Request"
		}
		return resp, err
	})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
		t.Errorf("accepted failed HTTP400 movement reads as confirmation (POSTs=%d)", requests.sends.Load())
	}
}

func TestDiscoveryWatermarkRejectsMissingFleetRows(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
	original := bot.device.GetClient().Transport
	bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(req)
		if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" && requests.sends.Load() == 0 {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			bodyText := strings.Replace(string(body), `<span class="fleetSlots"><span class="current">0</span>`, `<span class="fleetSlots"><span class="current">1</span>`, 1)
			resp.Body = io.NopCloser(strings.NewReader(bodyText))
		}
		return resp, err
	})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
		t.Errorf("accepted incomplete before watermark: slots=1, parsed fleets=0 (POSTs=%d)", requests.sends.Load())
	}
	if requests.sends.Load() != 0 {
		t.Errorf("incomplete watermark caused %d dispatches", requests.sends.Load())
	}
}

func TestDiscoveryCompleteWatermarkAllowsLegacyUnknownDirection(t *testing.T) {
	old := strings.Replace(discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet"), `data-return-flight="false"`, `data-return-flight=""`, 1)
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{before: old})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err != nil {
		t.Errorf("complete ID watermark blocked explicit JSON success: %v", err)
	}
	if requests.sends.Load() != 1 {
		t.Errorf("dispatch POSTs = %d, want 1", requests.sends.Load())
	}
}

func TestDiscoveryMovementAcceptsSourceShapedIngamePageBody(t *testing.T) {
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")}})
	original := bot.device.GetClient().Transport
	bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(req)
		if err == nil && req.Method == http.MethodGet && req.URL.Query().Get("component") == "movement" {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			// The source fixture uses a generic body ID and a head script to
			// identify movement. Every value here remains synthetic.
			page := strings.Replace(string(body), `</head><body id="movement"><script>var currentPage = "movement"; var token = "movement-token";</script>`, `<script>var currentPage = "movement";</script></head><body id="ingamepage">`, 1)
			resp.Body = io.NopCloser(strings.NewReader(page))
		}
		return resp, err
	})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err != nil {
		t.Errorf("source-shaped movement view was rejected: %v", err)
	}
	if requests.sends.Load() != 1 {
		t.Errorf("dispatch POSTs = %d, want 1", requests.sends.Load())
	}
}

func TestDiscoveryExplicitMoonOriginCanProveNewFleet(t *testing.T) {
	newFleet := strings.Replace(discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet"), `<span class="originPlanet"><figure class="planet">`, `<span class="originPlanet"><figure class="moon">`, 1)
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", after: []string{newFleet}})
	bot.cache.planets.Store([]Planet{{Planet: ogame.Planet{ID: 2, Coordinate: ogame.NewPlanetCoordinate(1, 1, 1)}, Moon: &Moon{Moon: ogame.Moon{ID: 1, Coordinate: ogame.NewMoonCoordinate(1, 1, 1)}}}})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err != nil {
		t.Errorf("valid explicit moon origin was not reconciled: %v", err)
	}
	if requests.sends.Load() != 1 {
		t.Errorf("dispatch POSTs = %d, want 1", requests.sends.Load())
	}
}

func TestDiscoveryMovementAllowsMatchingMobileDetailLinks(t *testing.T) {
	old := discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet")
	newFleet := discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet")
	old = strings.Replace(old, `</div>`, `<a class="openCloseDetails" data-mission-id="40"></a></div>`, 1)
	newFleet = strings.Replace(newFleet, `</div>`, `<a class="openCloseDetails" data-mission-id="41"></a></div>`, 1)
	bot, requests := newDiscoveryTestBot(t, discoveryFixture{status: http.StatusFound, location: "/game/index.php?page=ingame&component=overview", before: old, after: []string{old + newFleet}})
	if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err != nil {
		t.Errorf("matching mobile detail links were rejected: %v", err)
	}
	if requests.sends.Load() != 1 {
		t.Errorf("dispatch POSTs = %d, want 1", requests.sends.Load())
	}
}

func TestDiscoveryMovementRejectsInconsistentDetailLinks(t *testing.T) {
	for _, idAttribute := range []string{`data-mission-id="41"`, `data-mission-id="unknown"`, `data-mission-id="0"`, ""} {
		t.Run(idAttribute, func(t *testing.T) {
			old := discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet")
			old = strings.Replace(old, `</div>`, `<a class="openCloseDetails" `+idAttribute+`></a></div>`, 1)
			bot, requests := newDiscoveryTestBot(t, discoveryFixture{before: old})
			if err := bot.sendDiscoveryFleet(1, ogame.NewPlanetCoordinate(1, 1, 2), SkipRetry, SkipInterceptor); err == nil {
				t.Error("inconsistent detail-link IDs were accepted as a watermark")
			}
			if requests.sends.Load() != 0 {
				t.Errorf("invalid watermark caused %d dispatches", requests.sends.Load())
			}
		})
	}
}
