package wrapper

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alaingilbert/ogame/pkg/ogame"
)

func TestNativeSystemDiscoveryRejectsContradictoryAndInvalidResults(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"status":"failure","sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`,
		`{"success":true,"errors":[{"message":"rejected","error":100}],"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`,
		`{"success":true,"error":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`,
		`{"success":false,"response":{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}}`,
		`{"success":true,"sentToCoordinates":[{"galaxy":0,"system":1,"position":2}]}`,
		`{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":16}]}`,
		`{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2},{"galaxy":1,"system":1,"position":2}]}`,
	} {
		if coordinates, err := parseSystemDiscoveryActionResponse([]byte(body)); err == nil {
			t.Errorf("accepted invalid native result %s as %v", body, coordinates)
		}
	}
}

func TestNativeSystemDiscoveryCannotUsePreexistingOrIncompleteFleetEvidence(t *testing.T) {
	for _, test := range []struct {
		name, before, after string
		wantSuccess         bool
		response            string
		status              int
	}{
		{"preexisting changed route", discoveryFleetHTML(40, 3, false, "1:1:1", "1:1:8", "planet"), discoveryFleetHTML(40, 18, false, "1:1:1", "1:1:2", "planet"), false, "", 0},
		{"missing direction", "", `<div class="fleetDetails" data-mission-type="18" data-arrival-time="2000000000"><a class="openCloseDetails" data-mission-id="41" data-end-time="2000000000"></a><span class="originCoords"><a>[1:1:1]</a></span><span class="originPlanet"><figure class="planet"></figure></span><span class="destinationCoords"><a>[1:1:2]</a></span><span class="destinationPlanet"><figure class="planet"></figure></span></div>`, false, "", 0},
		{"unique new matching", "", discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet"), true, "", 0},
		{"positive JSON without evidence", "", "", false, `{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`, 0},
		{"positive JSON with evidence", "", discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet"), true, `{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`, 0},
		{"positive JSON from wrong system", "", discoveryFleetHTML(41, 18, false, "1:2:1", "1:2:2", "planet"), false, `{"success":true,"sentToCoordinates":[{"galaxy":1,"system":2,"position":2}]}`, 0},
		{"positive JSON at error HTTP status", "", discoveryFleetHTML(41, 18, false, "1:1:1", "1:1:2", "planet"), false, `{"success":true,"sentToCoordinates":[{"galaxy":1,"system":1,"position":2}]}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			posts := 0
			b := newCompatibilityTestBot(t, "13.0.0", func(req *http.Request) (*http.Response, error) {
				switch req.URL.Query().Get("component") {
				case "galaxy":
					return compatibilityResponse(req, `{"system":{"canSendSystemDiscovery":true,"galaxyContent":[{"position":2,"availableMissions":[{"missionType":18,"canSend":true}]}]},"token":"page-token"}`)
				case "movement":
					fleets := test.before
					if posts > 0 {
						fleets = test.after
					}
					page := discoveryMovementPage(fleets)
					page = strings.Replace(page, "</body>", `<div class="smallplanet" id="planet-1"><a class="planetlink" title="Fixture [1:1:1]12.800km (1/100) -10°C to 10°C"></a></div></body>`, 1)
					return compatibilityResponse(req, page)
				case "fleetdispatch":
					posts++
					if test.response != "" {
						resp, _ := compatibilityResponse(req, test.response)
						if test.status != 0 {
							resp.StatusCode = test.status
						}
						return resp, nil
					}
					resp, _ := compatibilityResponse(req, "")
					resp.StatusCode = http.StatusFound
					resp.Status = "302 Found"
					resp.Header.Set("Location", "/game/index.php?page=ingame&component=overview")
					return resp, nil
				}
				return nil, fmt.Errorf("unexpected request %s", req.URL)
			})
			coordinates, err := b.sendSystemDiscoveryFleet(ogame.CelestialID(1), 1, 1, SkipRetry, SkipInterceptor)
			if (test.wantSuccess && (err != nil || len(coordinates) != 1)) || (!test.wantSuccess && (err == nil || len(coordinates) > 0)) || posts != 1 {
				t.Fatalf("coordinates=%v error=%v posts=%d", coordinates, err, posts)
			}
		})
	}
}
