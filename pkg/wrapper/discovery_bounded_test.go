package wrapper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/labstack/echo/v4"
)

type boundedDiscoveryFixture struct {
	galaxyReads, reads, nativeCalls                          int
	sent                                                     []int64
	fleets, before                                           string
	ineligibleAt, rejectAt, unprovenAt                       int64
	ambiguous, unrelated, reused, httpError, invalidSnapshot bool
}

func newBoundedDiscoveryBot(t *testing.T, f *boundedDiscoveryFixture) *OGame {
	t.Helper()
	b := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		switch query.Get("component") {
		case "galaxy":
			f.galaxyReads++
			// Eligibility grows between successive reads. No new target is in
			// the caller's reservation, so it must never receive a dispatch.
			positions := []string{}
			for p := int64(2); p <= 5+int64(f.galaxyReads); p++ {
				canSend := "true"
				if p == f.ineligibleAt && len(f.sent) > 0 {
					canSend = "false"
				}
				positions = append(positions, fmt.Sprintf(`{"position":%d,"availableMissions":[{"missionType":18,"canSend":%s}]}`, p, canSend))
			}
			return compatibilityResponse(req, `{"system":{"canSendSystemDiscovery":true,"galaxyContent":[`+strings.Join(positions, ",")+`]},"token":"local-page-token"}`)
		case "movement":
			f.reads++
			page := discoveryMovementPage(f.before + f.fleets)
			if f.invalidSnapshot {
				page = strings.Replace(page, `<span class="current">0</span>`, `<span class="current">1</span>`, 1)
			}
			return compatibilityResponse(req, page)
		case "fleetdispatch":
			if query.Get("action") == "sendSystemDiscoveryFleet" {
				f.nativeCalls++
				return nil, fmt.Errorf("native bulk action escaped reservation")
			}
			if query.Get("action") != "sendDiscoveryFleet" {
				return nil, fmt.Errorf("unexpected action: %s", query.Get("action"))
			}
			if err := req.ParseForm(); err != nil {
				return nil, err
			}
			var position int64
			_, _ = fmt.Sscan(req.PostForm.Get("position"), &position)
			f.sent = append(f.sent, position)
			if req.PostForm.Get("token") != "local-page-token" || query.Get("cp") != "1" {
				t.Errorf("wrong origin/token: %s %v", req.URL, req.PostForm)
			}
			if position == f.rejectAt {
				return compatibilityResponse(req, `{"success":false,"message":"synthetic refusal"}`)
			}
			if position != f.unprovenAt {
				id := 40 + len(f.sent)
				if f.reused {
					id = 40
				}
				destination := fmt.Sprintf("1:1:%d", position)
				if f.unrelated {
					destination = "1:1:15"
				}
				fleet := discoveryFleetHTML(id, 18, false, "1:1:1", destination, "planet")
				if f.reused {
					f.before = ""
				}
				f.fleets += fleet
				if f.ambiguous {
					f.fleets += discoveryFleetHTML(id+100, 18, false, "1:1:1", destination, "planet")
				}
			}
			response, err := compatibilityResponse(req, `{"success":true}`)
			if f.httpError {
				response.StatusCode = http.StatusInternalServerError
				response.Status = "500 Internal Server Error"
			}
			return response, err
		}
		return nil, fmt.Errorf("unexpected fixture request: %s", req.URL)
	})
	b.cache.planets.Store([]Planet{{Planet: ogame.Planet{ID: 1, Coordinate: ogame.NewPlanetCoordinate(1, 1, 1)}}})
	return b
}

func TestBoundedDiscoveryHonorsReservedPositionsAsEligibilityGrows(t *testing.T) {
	f := &boundedDiscoveryFixture{}
	b := newBoundedDiscoveryBot(t, f)
	result, err := b.sendSystemDiscoveryFleetBounded(1, 1, 1, 2, []int64{2, 3}, SkipRetry, SkipInterceptor)
	if err != nil || !result.Complete || result.Error != "" || len(result.Coordinates) != 2 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !reflect.DeepEqual(f.sent, []int64{2, 3}) || f.nativeCalls != 0 {
		t.Fatalf("sent=%v native=%d", f.sent, f.nativeCalls)
	}
	for i, p := range []int64{2, 3} {
		if !result.Coordinates[i].Equal(ogame.NewPlanetCoordinate(1, 1, p)) {
			t.Errorf("unexpected coordinates=%v", result.Coordinates)
		}
	}
}

func TestBoundedDiscoveryPreservesPartialProofAndStopsOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    boundedDiscoveryFixture
	}{
		{"eligibility shrinks", boundedDiscoveryFixture{ineligibleAt: 3}},
		{"second rejected", boundedDiscoveryFixture{rejectAt: 3}},
		{"second unproven", boundedDiscoveryFixture{unprovenAt: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			b := newBoundedDiscoveryBot(t, &f)
			result, err := b.sendSystemDiscoveryFleetBounded(1, 1, 1, 3, []int64{2, 3, 4}, SkipRetry, SkipInterceptor)
			if err != nil || result.Complete || result.Error == "" || !reflect.DeepEqual(result.Coordinates, []ogame.Coordinate{ogame.NewPlanetCoordinate(1, 1, 2)}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			for _, p := range f.sent {
				if p == 4 {
					t.Fatalf("continued after uncertainty: %v", f.sent)
				}
			}
		})
	}
}

func TestBoundedDiscoveryRejectsUnrelatedAmbiguousOrExistingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    boundedDiscoveryFixture
	}{
		{"unrelated", boundedDiscoveryFixture{unrelated: true}},
		{"ambiguous", boundedDiscoveryFixture{ambiguous: true}},
		{"preexisting id changed route", boundedDiscoveryFixture{reused: true, before: discoveryFleetHTML(40, 3, false, "1:1:1", "1:1:8", "planet")}},
		{"http error after accepted action", boundedDiscoveryFixture{httpError: true}},
		{"incomplete initial snapshot", boundedDiscoveryFixture{invalidSnapshot: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f
			b := newBoundedDiscoveryBot(t, &f)
			result, err := b.sendSystemDiscoveryFleetBounded(1, 1, 1, 2, []int64{2, 3}, SkipRetry, SkipInterceptor)
			if err != nil || result.Complete || result.Error == "" || len(result.Coordinates) != 0 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if len(f.sent) > 1 {
				t.Fatalf("replayed or continued unsafe send: %v", f.sent)
			}
		})
	}
}

func TestBoundedDiscoveryRejectsInvalidReservationsBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		max       int64
		positions []int64
	}{
		{"missing", 0, nil}, {"negative", -1, []int64{2}}, {"too many reserved", 1, []int64{2, 3}},
		{"duplicate", 2, []int64{2, 2}}, {"zero position", 1, []int64{0}}, {"position sixteen", 1, []int64{16}},
		{"no positions", 1, nil}, {"excessive max", 16, []int64{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &boundedDiscoveryFixture{}
			b := newBoundedDiscoveryBot(t, f)
			_, err := b.sendSystemDiscoveryFleetBounded(1, 1, 1, tc.max, tc.positions, SkipRetry, SkipInterceptor)
			if err == nil || f.galaxyReads != 0 || f.reads != 0 || len(f.sent) != 0 {
				t.Fatalf("err=%v requests=%+v", err, f)
			}
		})
	}
}

func boundedHandlerRequest(t *testing.T, bot *OGame, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/bot/planets/1/send-system-discovery-bounded", strings.NewReader(values.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("planetID")
	c.SetParamValues("1")
	c.Set("bot", bot)
	if err := SendSystemDiscoveryBoundedHandler(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestBoundedDiscoveryHandlerRequiresBoundsAndPreservesPartialResult(t *testing.T) {
	for _, positions := range []string{"", "2,2", "2,bad", "2,,3", "0", "16"} {
		f := &boundedDiscoveryFixture{}
		b := newBoundedDiscoveryBot(t, f)
		rec := boundedHandlerRequest(t, b, url.Values{"galaxy": {"1"}, "system": {"1"}, "maxFleets": {"2"}, "positions": {positions}})
		if rec.Code != http.StatusBadRequest || f.galaxyReads != 0 {
			t.Fatalf("invalid %q: HTTP%d requests=%+v", positions, rec.Code, f)
		}
	}
	f := &boundedDiscoveryFixture{rejectAt: 3}
	b := newBoundedDiscoveryBot(t, f)
	rec := boundedHandlerRequest(t, b, url.Values{"galaxy": {"1"}, "system": {"1"}, "maxFleets": {"2"}, "positions": {"2,3"}})
	var response struct {
		Result struct {
			Coordinates []ogame.Coordinate `json:"coordinates"`
			Complete    bool               `json:"complete"`
			Error       string             `json:"error"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || response.Result.Complete || response.Result.Error == "" || len(response.Result.Coordinates) != 1 {
		t.Fatalf("HTTP%d %s", rec.Code, rec.Body)
	}
}
