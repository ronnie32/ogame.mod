package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/httpclient"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/hashicorp/go-version"
	"github.com/labstack/echo/v4"
)

// These fixtures describe public protocol shapes; every request is intercepted.
func newCompatibilityTestBot(t *testing.T, serverVersion string, respond func(*http.Request) (*http.Response, error)) *OGame {
	t.Helper()
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{Transport: fleetTargetTransport(respond)}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	bot.isLoggedInAtom.Store(true)
	bot.cache.serverURL = "https://ogame.invalid"
	bot.cache.serverData.Version = serverVersion
	bot.extractor = getExtractorFor(version.Must(version.NewVersion(sanitizeServerVersion(serverVersion))))
	bot.extractor.SetLocation(time.UTC)
	bot.cache.planets.Store([]Planet{{Planet: ogame.Planet{ID: 1, Coordinate: ogame.DoParseCoord("1:1:1")}}})
	return bot
}

func compatibilityResponse(req *http.Request, body string) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}
func compatibilityPage(component, content string) string {
	return `<html><head><meta name="ogame-session" content="fixture"/></head><body id="` + component + `"><script>var currentPage = "` + component + `"; var token = "fixturetoken";</script>` + content + `</body></html>`
}

func TestCompatibilityGalaxyReturnsModernResponseOnce(t *testing.T) {
	for _, ver := range []string{"13.0.0-r8", "12.0.0"} {
		t.Run(ver, func(t *testing.T) {
			calls := 0
			bot := newCompatibilityTestBot(t, ver, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Host != "ogame.invalid" {
					t.Fatalf("unexpected host: %s", req.URL.Host)
				}
				want := "fetchSolarSystemData"
				if ver == "12.0.0" {
					want = "fetchGalaxyContent"
				}
				if req.URL.Query().Get("action") != want || req.URL.Query().Get("cp") != "1" {
					t.Errorf("wrong request: %s", req.URL)
				}
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.PostForm.Get("galaxy") != "1" || req.PostForm.Get("system") != "2" {
					t.Errorf("wrong coordinates: %v", req.PostForm)
				}
				return compatibilityResponse(req, `{"system":{"galaxyContent":[{"position":3,"availableMissions":[{"canSend":true,"missionType":18}]}]},"token":"modern"}`)
			})
			got, err := bot.getGalaxyPage(1, 2, ChangePlanet(1), SkipRetry, SkipInterceptor)
			if err != nil || got.Token != "modern" || len(got.System.GalaxyContent) != 1 {
				t.Fatalf("response=%+v error=%v", got, err)
			}
			if calls != 1 {
				t.Errorf("galaxy requests=%d, want1", calls)
			}
		})
	}
}

func TestCompatibilityGalaxyRejectsInvalidEnvelope(t *testing.T) {
	for _, body := range []string{`{"status":"failure","message":"denied","system":{},"token":"t"}`, `{"success":false,"system":{},"token":"t"}`, `{"errors":[{"message":"denied"}],"system":{},"token":"t"}`, `{}`, `null`, `{"system":null,"token":"t"}`, `{"system":{}}`, `{"system":{},"token":"t"}`, `{"system":{"galaxyContent":null},"token":"t"}`} {
		t.Run(body, func(t *testing.T) {
			bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) { return compatibilityResponse(req, body) })
			if _, err := bot.getGalaxyPage(1, 2, SkipRetry, SkipInterceptor); err == nil {
				t.Fatal("accepted invalid galaxy response")
			}
		})
	}
}

func TestCompatibilityRecallDoesNotFallThrough(t *testing.T) {
	for _, ver := range []string{"13.0.0", "12.0.0"} {
		t.Run(ver, func(t *testing.T) {
			recalls := 0
			bot := newCompatibilityTestBot(t, ver, func(req *http.Request) (*http.Response, error) {
				q := req.URL.Query()
				if q.Get("action") == "recallFleet" || q.Get("return") != "" {
					recalls++
					if ver == "13.0.0" && (req.Method != http.MethodPost || q.Get("action") != "recallFleet") {
						t.Errorf("legacy recall after modern: %s %s", req.Method, req.URL)
					}
					if ver == "12.0.0" && (req.Method != http.MethodGet || q.Get("return") != "42") {
						t.Errorf("wrong legacy recall: %s", req.URL)
					}
					if req.Method == http.MethodGet {
						return compatibilityResponse(req, compatibilityPage("movement", ""))
					}
					return compatibilityResponse(req, `{"status":"success","success":true}`)
				}
				return compatibilityResponse(req, compatibilityPage("movement", `<div id="fleet42"><a class="icon_link" href="?token=legacytoken" data-recall-action="action=recallFleet"></a></div>`))
			})
			if err := bot.cancelFleet(42); err != nil {
				t.Fatal(err)
			}
			if recalls != 1 {
				t.Errorf("recall requests=%d, want1", recalls)
			}
		})
	}
}

func TestCompatibilityRecallRejectsServerFailure(t *testing.T) {
	for _, body := range []string{`{"status":"failure","message":"denied"}`, `{"status":"success","success":false}`, `{"status":"success","error":true}`, `{"status":"success","errors":[{"message":"denied"}]}`, `{}`, `null`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					return compatibilityResponse(req, body)
				}
				return compatibilityResponse(req, compatibilityPage("movement", `<div id="fleet42"><a class="icon_link" href="?token=legacytoken"></a></div>`))
			})
			if err := bot.cancelFleet(42); err == nil {
				t.Fatal("accepted failed recall")
			}
		})
	}
}
func TestCompatibilityRecallHandlerPropagatesError(t *testing.T) {
	bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return compatibilityResponse(req, `{"status":"failure","message":"denied"}`)
		}
		return compatibilityResponse(req, compatibilityPage("movement", `<div id="fleet42"><a class="icon_link" href="?token=legacytoken"></a></div>`))
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/fleets/42/cancel", nil)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("bot", bot)
	c.SetParamNames("fleetID")
	c.SetParamValues("42")
	if err := CancelFleetHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "denied") {
		t.Fatalf("handler returned%d %s", rec.Code, rec.Body)
	}
}

func TestCompatibilityEventsRejectInvalidEnvelope(t *testing.T) {
	for _, body := range []string{`{"content":`, `{}`, `null`, `{"content":{"eventlist":null}}`, `{"success":false,"content":{"eventlist":"<div id=\"eventListWrap\"></div>"}}`, `{"status":"failure","content":{"eventlist":"<div id=\"eventListWrap\"></div>"}}`} {
		t.Run(body, func(t *testing.T) {
			bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) { return compatibilityResponse(req, body) })
			if _, err := bot.getAttacks(SkipRetry, SkipInterceptor); err == nil {
				t.Fatal("accepted invalid event list")
			}
		})
	}
}
func TestCompatibilityEventsPreserveOptionsAndHostiles(t *testing.T) {
	fixture, err := os.ReadFile("../../samples/v11.16.18/en/eventlist_acs_attack_unknown_ships.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, ver := range []string{"13.0.0", "12.0.0"} {
		t.Run(ver, func(t *testing.T) {
			bot := newCompatibilityTestBot(t, ver, func(req *http.Request) (*http.Response, error) {
				if req.URL.Query().Get("cp") != "1" {
					t.Errorf("lost ChangePlanet option: %s", req.URL)
				}
				if ver == "13.0.0" {
					b, _ := json.Marshal(map[string]any{"content": map[string]any{"eventlist": string(fixture)}})
					return compatibilityResponse(req, string(b))
				}
				return compatibilityResponse(req, string(fixture))
			})
			attacks, err := bot.getAttacks(ChangePlanet(1), SkipRetry, SkipInterceptor)
			if err != nil || len(attacks) == 0 {
				t.Fatalf("attacks=%d err=%v", len(attacks), err)
			}
		})
	}
}
func TestCompatibilityEventsAcceptEmptyList(t *testing.T) {
	bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
		return compatibilityResponse(req, `{"content":{"eventlist":"<div id=\"eventListWrap\"></div>"}}`)
	})
	got, err := bot.getAttacks(SkipRetry, SkipInterceptor)
	if err != nil || len(got) != 0 {
		t.Fatalf("attacks=%v err=%v", got, err)
	}
}

func TestCompatibilityCaptchaJSONAndErrors(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		failSolve, failLogin bool
		want                 int
	}{{"solved", false, false, 200}, {"solve failed", true, false, 502}, {"login failed", false, true, 500}} {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
				requests++
				if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/fixtureid/en-GB") {
					return nil, fmt.Errorf("unexpected captcha request %s", req.URL)
				}
				resp, _ := compatibilityResponse(req, `{}`)
				if tt.failSolve {
					resp.StatusCode = 400
					resp.Status = "400 Bad Request"
				}
				return resp, nil
			})
			loginCalls := 0
			bot.loginWrapper = func(LoginFn) error { loginCalls++; return fmt.Errorf("synthetic login failure") }
			if tt.failLogin {
				bot.isLoggedInAtom.Store(false)
			}
			req := httptest.NewRequest(http.MethodPost, "/bot/captcha/solve", strings.NewReader("challenge_id=fixtureid&answer=1"))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			c.Set("bot", bot)
			if err := GetCaptchaSolverHandler(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.want || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("response=%d %s", rec.Code, rec.Body)
			}
			if requests != 1 {
				t.Errorf("requests=%d", requests)
			}
			if tt.failLogin && loginCalls != 1 {
				t.Errorf("login calls=%d", loginCalls)
			}
		})
	}
}

func TestCompatibilityTraderRoutesAndTokenRotation(t *testing.T) {
	for _, ver := range []string{"13.0.0", "12.0.0"} {
		t.Run(ver, func(t *testing.T) {
			step := 0
			offer := `<div class="js_import_price">100</div><script>var planetResources = {"1":{"input":{"metal":1000,"crystal":0,"deuterium":0}}}; var multiplier = {"metal":1,"crystal":1,"deuterium":1}; var token = "importtoken";</script>`
			bot := newCompatibilityTestBot(t, ver, func(req *http.Request) (*http.Response, error) {
				step++
				if req.URL.Host != "ogame.invalid" {
					return nil, fmt.Errorf("unexpected host")
				}
				_ = req.ParseForm()
				q, p := req.URL.Query(), req.PostForm
				if ver == "13.0.0" {
					if q.Get("component") != "trader" {
						t.Errorf("step%d component=%s", step, q.Get("component"))
					}
					switch step {
					case 1:
						if req.Method != http.MethodGet {
							t.Errorf("initial trader method=%s", req.Method)
						}
						return compatibilityResponse(req, compatibilityPage("trader", ""))
					case 2:
						if p.Get("action") != "importexport" || p.Get("token") != "fixturetoken" {
							t.Errorf("import request=%v", p)
						}
						b, _ := json.Marshal(map[string]any{"content": map[string]string{"trader": offer}, "newAjaxToken": "importtoken"})
						return compatibilityResponse(req, string(b))
					case 3:
						if p.Get("action") != "importExportTrade" || p.Get("token") != "importtoken" || p.Get("bid[planets][1][metal]") != "100" {
							t.Errorf("trade request=%v", p)
						}
						return compatibilityResponse(req, `{"success":true,"newAjaxToken":"taketoken"}`)
					case 4:
						if p.Get("action") != "importExportTakeItem" || p.Get("token") != "taketoken" {
							t.Errorf("take request=%v", p)
						}
						return compatibilityResponse(req, `{"success":true}`)
					}
				} else {
					if q.Get("component") != "traderimportexport" {
						t.Errorf("legacy component=%s", q.Get("component"))
					}
					switch step {
					case 1:
						return compatibilityResponse(req, offer)
					case 2:
						if p.Get("action") != "trade" || p.Get("token") != "importtoken" {
							t.Errorf("legacy trade request=%v", p)
						}
						return compatibilityResponse(req, `{"error":false,"newToken":"taketoken"}`)
					case 3:
						if p.Get("action") != "takeItem" || p.Get("token") != "taketoken" {
							t.Errorf("legacy take request=%v", p)
						}
						return compatibilityResponse(req, `{"error":false}`)
					}
				}
				return nil, fmt.Errorf("unexpected request%d", step)
			})
			if err := bot.buyOfferOfTheDay(); err != nil {
				t.Fatal(err)
			}
			want := 4
			if ver == "12.0.0" {
				want = 3
			}
			if step != want {
				t.Errorf("requests=%d want%d", step, want)
			}
		})
	}
}
func TestCompatibilityTraderRejectsFailure(t *testing.T) {
	for _, body := range []string{`{"success":false,"message":"denied"}`, `{"success":true,"errors":[{"message":"denied"}]}`, `{"success":true,"error":true}`, `{"status":"failure","success":true}`, `{"success":true}`, `{}`, `null`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) { return compatibilityResponse(req, body) })
			if _, err := bot.traderImportExportTrade(0, "importtoken", nil, ogame.Multiplier{}); err == nil {
				t.Error("accepted trade with no positive result or next token")
			}
			if body != `{"success":true}` {
				if err := bot.traderImportExportTakeItem("taketoken"); err == nil {
					t.Error("accepted failed takeItem")
				}
			}
		})
	}
}
func TestCompatibilityTraderMissingTokenStopsPurchase(t *testing.T) {
	calls := 0
	bot := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return compatibilityResponse(req, compatibilityPage("trader", ""))
		}
		return compatibilityResponse(req, `{"content":{"trader":"<div class=\"js_import_price\">100</div><script>var planetResources = {}; var multiplier = {};</script>"}}`)
	})
	if err := bot.buyOfferOfTheDay(); err == nil {
		t.Fatal("accepted missing import token")
	}
	if calls > 2 {
		t.Errorf("continued purchase after missing token: %d", calls)
	}
}

func TestCompatibilityLifeformVersionDispatch(t *testing.T) {
	html := []byte(`<bonus-item-content data-toggable-target="ships"><bonus-item-content-holder><inner-bonus-item-heading data-toggable="202"><bonus-items><bonus-item>-</bonus-item><bonus-item>-</bonus-item><bonus-item>-</bonus-item><bonus-item>1%</bonus-item></bonus-items></inner-bonus-item-heading></bonus-item-content-holder></bonus-item-content>`)
	for _, tt := range []struct {
		ver  string
		want float64
	}{{"12.0.0", 0.01}, {"13.0.0", 0.01}} {
		t.Run(tt.ver, func(t *testing.T) {
			ext := getExtractorFor(version.Must(version.NewVersion(tt.ver)))
			b, err := ext.ExtractLfBonuses(html)
			if err != nil {
				t.Fatal(err)
			}
			if got := b.LfShipBonuses[ogame.SmallCargoID].Speed; got != tt.want {
				t.Errorf("speed=%v want%v", got, tt.want)
			}
		})
	}
}
