package wrapper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/device"
	"github.com/alaingilbert/ogame/pkg/extractor/v12_0_0"
	"github.com/alaingilbert/ogame/pkg/httpclient"
	"github.com/alaingilbert/ogame/pkg/ogame"
)

type pr161Transport func(*http.Request) (string, error)

func (f pr161Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "ogame.invalid" {
		return nil, fmt.Errorf("unexpected host: %s", req.URL.Host)
	}
	body, err := f(req)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func newPR161Bot(t *testing.T, serverVersion string, respond pr161Transport) *OGame {
	t.Helper()
	dev := &device.Device{}
	dev.SetClient(&httpclient.Client{Client: &http.Client{Transport: respond}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	bot, err := NewWithParams(Params{Device: dev, Ctx: ctx, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	bot.isLoggedInAtom.Store(true)
	bot.cache.serverURL = "https://ogame.invalid"
	bot.cache.serverData.Version = serverVersion
	bot.extractor = v12_0_0.NewExtractor()
	return bot
}

// Synthetic component fixtures mirror the existing per-page extractors. Every
// request stays inside the RoundTripper, including any unexpected fallback.
func techsComponentFixture(component, id string) string {
	content := map[string]string{
		"supplies":    `<span class="metalMine"><span class="level" data-value="30"></span></span>`,
		"facilities":  `<span class="roboticsFactory"><span class="level" data-value="9"></span></span><span class="moonbase"><span class="level" data-value="7"></span></span>`,
		"shipyard":    `<span class="transporterSmall"><span class="amount" data-value="4"></span></span>`,
		"defenses":    `<span class="rocketLauncher"><span class="amount" data-value="5"></span></span>`,
		"research":    `<span class="espionageTechnology"><span class="level" data-value="6"></span></span>`,
		"lfbuildings": `<div id="lifeform"><a><div class="lifeform1"></div></a></div><span class="lifeformTech11101"><span class="level" data-value="12"></span></span>`,
		"lfresearch":  `<span class="lifeformTech11212"><span class="level" data-value="8"></span></span>`,
	}[component]
	if component == "supplies" && id == "2" {
		content = `<span class="metalStorage"><span class="level" data-value="4"></span></span>`
	}
	return fmt.Sprintf(`<html><head><meta name="ogame-session" content="fixture"/><meta name="ogame-planet-id" content="%s"/></head><body><script>var currentPage = "%s";</script>%s</body></html>`, id, component, content)
}

func addTechsCelestials(bot *OGame) {
	bot.cache.planets.Store([]Planet{convertPlanet(bot, ogame.Planet{ID: 1, Moon: &ogame.Moon{ID: 2}})})
	bot.extractor.SetLifeformEnabled(true)
}

func TestGetTechsV13WithoutCommander(t *testing.T) {
	for _, id := range []ogame.CelestialID{1, 2} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			var components []string
			bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
				q := req.URL.Query()
				component := q.Get("component")
				components = append(components, component)
				if req.Method != http.MethodGet || q.Get("page") != "ingame" || component == "empire" {
					t.Errorf("unexpected techs request: %s %s", req.Method, req.URL)
					return `{"1":30}`, nil
				}
				wantID := fmt.Sprint(id)
				if component == "research" {
					wantID = "1" // Account research is read on the moon's parent planet.
				}
				if q.Get("cp") != wantID {
					t.Errorf("%s cp = %q, want %q", component, q.Get("cp"), wantID)
				}
				return techsComponentFixture(component, q.Get("cp")), nil
			})
			addTechsCelestials(bot)
			got, err := bot.getTechs(id)
			if err != nil {
				t.Fatal(err)
			}
			if got.RoboticsFactory != 9 || got.SmallCargo != 4 || got.RocketLauncher != 5 || got.EspionageTechnology != 6 {
				t.Errorf("incomplete techs: robotics=%d, smallCargo=%d, rockets=%d, espionage=%d", got.RoboticsFactory, got.SmallCargo, got.RocketLauncher, got.EspionageTechnology)
			}
			want := []string{"supplies", "facilities", "shipyard", "defenses", "research"}
			if id == 1 {
				want = append(want, "lfbuildings", "lfresearch")
				if got.MetalMine != 30 || got.LifeformType != ogame.Humans || got.ResidentialSector != 12 || got.PlasmaTerraformer == nil || *got.PlasmaTerraformer != 8 {
					t.Errorf("incomplete planet techs: metalMine=%d, lifeform=%d, residential=%d, plasma=%v", got.MetalMine, got.LifeformType, got.ResidentialSector, got.PlasmaTerraformer)
				}
			} else if got.MetalMine != 0 || got.MetalStorage != 4 || got.LunarBase != 7 || !reflect.DeepEqual(got.LfBuildings, ogame.LfBuildings{}) || !reflect.DeepEqual(got.LfResearches, ogame.LfResearches{}) {
				t.Errorf("incorrect moon techs: metalMine=%d, storage=%d, lunarBase=%d, lifeform=%d", got.MetalMine, got.MetalStorage, got.LunarBase, got.LifeformType)
			}
			if !reflect.DeepEqual(components, want) {
				t.Errorf("components = %v, want %v", components, want)
			}
			if len(bot.getCachedCelestials()) != 2 || bot.cache.hasCommander {
				t.Fatal("techs request changed celestial cache or required Commander")
			}
		})
	}
}

func TestGetTechsV13InvalidCelestial(t *testing.T) {
	for _, id := range []ogame.CelestialID{0, -1, 999} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			calls := 0
			bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
				calls++
				return `{}`, nil
			})
			addTechsCelestials(bot)
			got, err := bot.getTechs(id)
			if !errors.Is(err, ogame.ErrInvalidPlanetID) || calls != 0 || !reflect.DeepEqual(got, ogame.Techs{}) {
				t.Fatalf("error=%v, requests=%d, resultIsZero=%v", err, calls, reflect.DeepEqual(got, ogame.Techs{}))
			}
		})
	}
}

func TestGetTechsV13RejectsIncorrectPages(t *testing.T) {
	for _, kind := range []string{"wrong celestial", "missing celestial", "wrong component", "missing component"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
				calls++
				if req.URL.Query().Get("page") == "fetchTechs" {
					return `{}`, nil
				}
				q := req.URL.Query()
				by := techsComponentFixture(q.Get("component"), q.Get("cp"))
				if q.Get("component") == "facilities" {
					switch kind {
					case "wrong celestial":
						by = strings.ReplaceAll(by, `ogame-planet-id" content="1`, `ogame-planet-id" content="2`)
					case "missing celestial":
						by = strings.ReplaceAll(by, `name="ogame-planet-id"`, `name="other"`)
					case "wrong component":
						by = strings.ReplaceAll(by, `currentPage = "facilities"`, `currentPage = "overview"`)
					case "missing component":
						by = strings.ReplaceAll(by, `currentPage`, `otherPage`)
					}
				}
				return by, nil
			})
			addTechsCelestials(bot)
			got, err := bot.getTechs(1)
			if err == nil || calls != 2 || !reflect.DeepEqual(got, ogame.Techs{}) {
				t.Fatalf("error=%v, requests=%d, resultIsZero=%v; want error after facilities without partial result", err, calls, reflect.DeepEqual(got, ogame.Techs{}))
			}
		})
	}
}

func TestGetTechsLegacyEndpoint(t *testing.T) {
	calls := 0
	bot := newPR161Bot(t, "12.0.43", func(req *http.Request) (string, error) {
		calls++
		q := req.URL.Query()
		if req.Method != http.MethodGet || q.Get("page") != "fetchTechs" || q.Get("cp") != "2" {
			t.Errorf("wrong legacy request: %s %s", req.Method, req.URL)
		}
		return `{"1":30,"14":9,"202":4,"401":5,"106":6}`, nil
	})
	addTechsCelestials(bot)
	got, err := bot.getTechs(2)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.MetalMine != 30 || got.RoboticsFactory != 9 || got.SmallCargo != 4 || got.RocketLauncher != 5 || got.EspionageTechnology != 6 {
		t.Fatalf("techs=%+v, requests=%d", got, calls)
	}
}

type techsLifeformExtractor struct{ *v12_0_0.Extractor }

func (e *techsLifeformExtractor) ExtractLfBuildingsFromDoc(doc *goquery.Document) (ogame.LfBuildings, error) {
	return ogame.LfBuildings{LifeformType: ogame.Humans, ResidentialSector: 77}, nil
}

func (e *techsLifeformExtractor) ExtractLfResearchFromDoc(doc *goquery.Document) (ogame.LfResearches, error) {
	level := int64(55)
	return ogame.LfResearches{PlasmaTerraformer: &level}, nil
}

func TestGetTechsUsesVersionSpecificLifeformExtractor(t *testing.T) {
	bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
		q := req.URL.Query()
		return techsComponentFixture(q.Get("component"), q.Get("cp")), nil
	})
	addTechsCelestials(bot)
	bot.extractor = &techsLifeformExtractor{bot.extractor.(*v12_0_0.Extractor)}
	got, err := bot.getTechs(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResidentialSector != 77 || got.PlasmaTerraformer == nil || *got.PlasmaTerraformer != 55 {
		t.Fatalf("version-specific lifeform extractor was bypassed: residential=%d, plasma=%v", got.ResidentialSector, got.PlasmaTerraformer)
	}
}

func TestGetTechsWithoutLifeforms(t *testing.T) {
	var components []string
	bot := newPR161Bot(t, "13.0.1", func(req *http.Request) (string, error) {
		q := req.URL.Query()
		components = append(components, q.Get("component"))
		return techsComponentFixture(q.Get("component"), q.Get("cp")), nil
	})
	addTechsCelestials(bot)
	bot.extractor.SetLifeformEnabled(false)
	got, err := bot.getTechs(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 5 || got.MetalMine != 30 || !reflect.DeepEqual(got.LfBuildings, ogame.LfBuildings{}) || !reflect.DeepEqual(got.LfResearches, ogame.LfResearches{}) {
		t.Fatalf("components=%v, metalMine=%d, lifeform=%d", components, got.MetalMine, got.LifeformType)
	}
}

var errTechsExtractor = errors.New("synthetic facilities extraction error")

type techsErrorExtractor struct{ *v12_0_0.Extractor }

func (e *techsErrorExtractor) ExtractFacilitiesFromDoc(doc *goquery.Document) (ogame.Facilities, error) {
	return ogame.Facilities{}, errTechsExtractor
}

func TestGetTechsPropagatesExtractionError(t *testing.T) {
	calls := 0
	bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
		calls++
		q := req.URL.Query()
		return techsComponentFixture(q.Get("component"), q.Get("cp")), nil
	})
	addTechsCelestials(bot)
	bot.extractor = &techsErrorExtractor{bot.extractor.(*v12_0_0.Extractor)}
	got, err := bot.getTechs(1)
	if !errors.Is(err, errTechsExtractor) || calls != 2 || !reflect.DeepEqual(got, ogame.Techs{}) {
		t.Fatalf("error=%v, requests=%d, resultIsZero=%v", err, calls, reflect.DeepEqual(got, ogame.Techs{}))
	}
}

func TestGetTechsPropagatesRequestCancellation(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bot := newPR161Bot(t, "13.0.0", func(req *http.Request) (string, error) {
		calls++
		q := req.URL.Query()
		if q.Get("component") == "facilities" {
			cancel()
			return "", context.Canceled
		}
		return techsComponentFixture(q.Get("component"), q.Get("cp")), nil
	})
	bot.parentCtx = ctx
	addTechsCelestials(bot)
	got, err := bot.getTechs(1)
	if !errors.Is(err, context.Canceled) || calls != 2 || !reflect.DeepEqual(got, ogame.Techs{}) {
		t.Fatalf("error=%v, requests=%d, resultIsZero=%v", err, calls, reflect.DeepEqual(got, ogame.Techs{}))
	}
}

func TestHighscoreVersionedEndpoint(t *testing.T) {
	for _, version := range []string{"12.0.43", "13.0.0-pl1"} {
		t.Run(version, func(t *testing.T) {
			calls := 0
			bot := newPR161Bot(t, version, func(req *http.Request) (string, error) {
				calls++
				q := req.URL.Query()
				if strings.HasPrefix(version, "13.") {
					if req.Method != http.MethodGet || q.Get("page") != "ingame" || q.Get("component") != "highscore" {
						t.Errorf("wrong modern highscore request: %s %s", req.Method, req.URL)
					}
				} else if req.Method != http.MethodPost || q.Get("page") != "highscoreContent" {
					t.Errorf("wrong legacy highscore request: %s %s", req.Method, req.URL)
				}
				if q.Get("category") != "1" || q.Get("type") != "8" || q.Get("site") != "2" {
					t.Errorf("lost highscore filters: %s", req.URL)
				}
				return `<html><head><meta name="ogame-session" content="fixture"/></head><body><div id="stat_list_content"><script>var site = 2; var currentCategory = 1; var currentType = 8;</script><table id="ranks"><tbody><tr id="position100"><td class="position">101</td><td class="name"><a href="?galaxy=1&amp;system=2&amp;position=3">home</a><span class="playername">Fixture</span></td><td class="score">123</td></tr></tbody></table></div></body></html>`, nil
			})
			got, err := bot.highscore(1, 8, 2)
			if err != nil {
				t.Fatal(err)
			}
			if got.CurrPage != 2 || len(got.Players) != 1 || got.Players[0].Position != 101 || calls != 1 {
				t.Fatalf("highscore = %+v, requests = %d", got, calls)
			}
		})
	}
}
