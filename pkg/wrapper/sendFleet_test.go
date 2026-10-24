package wrapper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/httpclient"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/labstack/echo/v4"
)

func TestSendFleetCheckTarget(t *testing.T) {
	// Synthetic protocol fixtures, not captured account data. The OGame 13
	// status/success form is documented in alaingilbert/ogame PR161, commit
	// 5681171; status/message=ok is covered by ogame.mod PR169, d8befa6.
	tests := []struct {
		name        string
		response    string
		wantSend    int
		wantMessage string
	}{
		{"legacy target", `{"targetOk":true}`, 1, "mock dispatch reached (4999)"},
		{"legacy with status", `{"targetOk":true,"status":"success"}`, 1, "mock dispatch reached (4999)"},
		{"v13 success", `{"status":"success","success":true}`, 1, "mock dispatch reached (4999)"},
		{"v13 message compatibility", `{"status":"success","message":"ok"}`, 1, "mock dispatch reached (4999)"},
		{"v13 empty errors", `{"status":"success","success":true,"errors":[],"error":false}`, 1, "mock dispatch reached (4999)"},
		{"legacy rejection", `{"targetOk":false}`, 0, "target is not ok"},
		{"explicit target rejection", `{"targetOk":false,"status":"success","success":true}`, 0, "target is not ok"},
		{"target rejection with ok message", `{"targetOk":false,"status":"success","message":"ok"}`, 0, "target is not ok"},
		{"success false", `{"status":"success","success":false}`, 0, "target is not ok"},
		{"success false overrides legacy", `{"targetOk":true,"success":false}`, 0, "target is not ok"},
		{"success false overrides message", `{"status":"success","success":false,"message":"ok"}`, 0, "target is not ok"},
		{"failure overrides success", `{"status":"failure","success":true}`, 0, "target is not ok"},
		{"failure overrides legacy", `{"targetOk":true,"status":"failure"}`, 0, "target is not ok"},
		{"failure overrides message", `{"status":"failure","message":"ok"}`, 0, "target is not ok"},
		{"errors override v13", `{"status":"success","success":true,"errors":[{"message":"Target unavailable","error":4049}]}`, 0, "Target unavailable (4049)"},
		{"errors override legacy", `{"targetOk":true,"errors":[{"message":"Target unavailable","error":4049}]}`, 0, "Target unavailable (4049)"},
		{"errors override message", `{"status":"success","message":"ok","errors":[{"message":"Target unavailable","error":4049}]}`, 0, "Target unavailable (4049)"},
		{"server message", `{"status":"failure","success":false,"message":"Target is protected"}`, 0, "Target is protected"},
		{"status alone", `{"status":"success"}`, 0, "target is not ok"},
		{"message alone", `{"message":"ok"}`, 0, "target is not ok"},
		{"success alone", `{"success":true}`, 0, "target is not ok"},
		{"unknown status", `{"status":"pending","success":true}`, 0, "target is not ok"},
		{"explicit error", `{"status":"success","success":true,"error":true,"message":"Request refused"}`, 0, "Request refused"},
		{"null target", `{"targetOk":null,"status":"success","success":true}`, 0, "target is not ok"},
		{"null success", `{"targetOk":true,"success":null}`, 0, "target is not ok"},
		{"string target", `{"targetOk":"true"}`, 0, "target is not ok"},
		{"string success", `{"targetOk":true,"success":"true"}`, 0, "target is not ok"},
		{"null response", `null`, 0, "target is not ok"},
		{"empty object", `{}`, 0, "target is not ok"},
		{"empty body", ``, 0, ""},
		{"array response", `[]`, 0, ""},
		{"invalid json", `{"status":`, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, requests := newFleetTargetTestBot(t, tt.response)
			_, err := bot.sendFleet(1, ogame.ShipsInfos{SmallCargo: 1}, ogame.HundredPercent,
				ogame.DoParseCoord("1:1:2"), ogame.Transport, ogame.Resources{}, 0, 0, false)
			if err == nil {
				t.Fatal("expected either target rejection or the mock dispatch error")
			}
			if tt.wantMessage != "" && !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error = %q, want %q", err, tt.wantMessage)
			}
			if requests.checkTarget != 1 || requests.sendFleet != tt.wantSend {
				t.Errorf("requests: checkTarget=%d, sendFleet=%d; want 1, %d",
					requests.checkTarget, requests.sendFleet, tt.wantSend)
			}
		})
	}
}

func TestSendFleetHandlerCheckTargetError(t *testing.T) {
	bot, requests := newFleetTargetTestBot(t,
		`{"status":"failure","success":false,"errors":[{"message":"Target unavailable","error":4049}]}`)
	req := httptest.NewRequest(http.MethodPost, "/bot/planets/1/send-fleet",
		strings.NewReader("ships=202,1&speed=10&galaxy=1&system=1&position=2&type=1&mission=3"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("planetID")
	c.SetParamValues("1")
	c.Set("bot", bot)
	if err := SendFleetHandler(c); err != nil {
		t.Fatal(err)
	}
	// The fake game endpoint always returns HTTP200. This 500 is generated
	// by the local API for a semantic checkTarget error, not by Gameforge.
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Target unavailable (4049)") {
		t.Fatalf("local response: %d %s", rec.Code, rec.Body.String())
	}
	if requests.checkTarget != 1 || requests.sendFleet != 0 {
		t.Fatalf("unexpected requests: %+v", requests)
	}
}

func TestSendFleetDoesNotReplayAcceptedPostWithLostOutcome(t *testing.T) {
	for _, outcome := range []string{"HTTP500", "lost response"} {
		t.Run(outcome, func(t *testing.T) {
			bot, requests := newFleetTargetTestBot(t, `{"targetOk":true}`)
			transport := bot.device.GetClient().Transport
			accepted := 0
			bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
				resp, err := transport.RoundTrip(req)
				if req.Method == http.MethodPost && req.URL.Query().Get("action") == "sendFleet" {
					accepted++ // The server applies the action before the outcome is lost.
					if accepted == 1 {
						_ = resp.Body.Close()
						if outcome == "lost response" {
							return nil, fmt.Errorf("synthetic connection lost after dispatch")
						}
						resp.StatusCode = http.StatusInternalServerError
						resp.Status = "500 Internal Server Error"
						resp.Body = io.NopCloser(strings.NewReader(`{"success":true}`))
					}
				}
				return resp, err
			})
			_, err := bot.sendFleet(1, ogame.ShipsInfos{SmallCargo: 1}, ogame.HundredPercent,
				ogame.NewPlanetCoordinate(1, 1, 2), ogame.Transport, ogame.Resources{}, 0, 0, false)
			if err == nil || requests.sendFleet != 1 || accepted != 1 {
				t.Fatalf("ambiguous dispatch was replayed: posts=%d accepted=%d error=%v", requests.sendFleet, accepted, err)
			}
		})
	}
}

func TestSendFleetCheckTargetReadRemainsRetryable(t *testing.T) {
	bot, requests := newFleetTargetTestBot(t, `{"targetOk":true}`)
	transport := bot.device.GetClient().Transport
	bot.device.GetClient().Transport = fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(req)
		if req.URL.Query().Get("action") == "checkTarget" && requests.checkTarget == 1 {
			resp.StatusCode = http.StatusInternalServerError
			resp.Status = "500 Internal Server Error"
		}
		return resp, err
	})
	_, err := bot.sendFleet(1, ogame.ShipsInfos{SmallCargo: 1}, ogame.HundredPercent,
		ogame.NewPlanetCoordinate(1, 1, 2), ogame.Transport, ogame.Resources{}, 0, 0, false)
	if err == nil || requests.checkTarget != 2 || requests.sendFleet != 1 {
		t.Fatalf("checks=%d sends=%d error=%v", requests.checkTarget, requests.sendFleet, err)
	}
}

type fleetTargetRequests struct {
	checkTarget int
	sendFleet   int
}

type fleetTargetTransport func(*http.Request) (*http.Response, error)

func (f fleetTargetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newFleetTargetTestBot(t *testing.T, checkResponse string) (*OGame, *fleetTargetRequests) {
	t.Helper()
	requests := &fleetTargetRequests{}
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{Transport: fleetTargetTransport(func(req *http.Request) (*http.Response, error) {
		// No request can leave this transport, even if production code changes.
		if req.URL.Host != "ogame.invalid" {
			return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
		}
		params := req.URL.Query()
		var body string
		switch {
		case req.Method == http.MethodGet && (params.Get("component") == "movement" || params.Get("component") == "fleetdispatch"):
			page := params.Get("component")
			body = fmt.Sprintf(`<html><head><meta name="ogame-session" content="fixture"/></head><body id="%s">
<script>var currentPage = "%s"; var token = "fixturetoken";</script>
<span class="fleetSlots"><span class="current">0</span><span class="all">10</span></span>
<span class="expSlots"><span class="current">0</span><span class="all">2</span></span>
<div id="fleetdispatchcomponent"><script>var shipsOnPlanet = [{"id":202,"number":1}];</script></div>
</body></html>`, page, page)
		case req.Method == http.MethodPost && params.Get("action") == "checkTarget":
			requests.checkTarget++
			body = checkResponse
		case req.Method == http.MethodPost && params.Get("action") == "sendFleet":
			requests.sendFleet++
			body = `{"success":false,"errors":[{"message":"mock dispatch reached","error":4999}]}`
		default:
			return nil, fmt.Errorf("unexpected game request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	bot.isLoggedInAtom.Store(true)
	bot.cache.serverURL = "https://ogame.invalid"
	bot.cache.researches = &ogame.Researches{}
	bot.cache.lfBonuses = &ogame.LfBonuses{}
	return bot, requests
}
