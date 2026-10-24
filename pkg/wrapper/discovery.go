package wrapper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/alaingilbert/ogame/pkg/utils"
)

// Adapted narrowly from Ronnie32/ogame.mod b7f9e90, 33d070c and a0c8113
// (MIT; Copyright (c) 2018 Alain Gilbert). See doc/discovery-protocol.md.
func isDiscoveryAction(method string, vals url.Values) bool {
	return method == http.MethodPost && vals.Get("page") == "ingame" &&
		vals.Get("component") == "fleetdispatch" &&
		(vals.Get("action") == "sendDiscoveryFleet" || vals.Get("action") == "sendSystemDiscoveryFleet")
}

func isInternalDiscoveryRedirect(location, referenceURL, serverURL string) bool {
	if strings.TrimSpace(location) == "" {
		return false
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	reference, err := url.Parse(referenceURL)
	if err != nil {
		return false
	}
	target, err := url.Parse(location)
	if err != nil {
		return false
	}
	target = reference.ResolveReference(target)
	if target.User != nil || !strings.EqualFold(target.Scheme, server.Scheme) ||
		!strings.EqualFold(target.Host, server.Host) || target.Path != "/game/index.php" {
		return false
	}
	// Only the observed in-game redirect forms are candidates for fleet
	// reconciliation. Neither an internal redirect nor its body proves dispatch.
	page, component := target.Query().Get("page"), target.Query().Get("component")
	return (page == "" && component == "") ||
		((page == "ingame" || page == "componentOnly") && (component == "overview" || component == "fleetdispatch"))
}

type discoveryResponse struct {
	Status   json.RawMessage `json:"status"`
	Success  json.RawMessage `json:"success"`
	Message  string          `json:"message"`
	Error    json.RawMessage `json:"error"`
	Errors   []OGameError    `json:"errors"`
	Response json.RawMessage `json:"response"`
}

func parseDiscoveryResponse(body []byte) error {
	var result discoveryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("invalid discovery response: %w", err)
	}
	if err := result.rejection(); err != nil {
		return err
	}
	if len(result.Response) != 0 {
		var nested discoveryResponse
		if err := json.Unmarshal(result.Response, &nested); err != nil {
			return fmt.Errorf("invalid discovery response: %w", err)
		}
		if err := nested.rejection(); err != nil {
			return err
		}
		if len(nested.Response) != 0 {
			return errors.New("invalid discovery response: unexpected nested response")
		}
		result = nested
	}
	if string(bytes.TrimSpace(result.Success)) != "true" {
		return errors.New("discovery response did not confirm success")
	}
	return nil
}

func (r discoveryResponse) rejection() error {
	status := string(bytes.TrimSpace(r.Status))
	success, errorValue := string(bytes.TrimSpace(r.Success)), string(bytes.TrimSpace(r.Error))
	if (status == "" || status == `"success"`) && (success == "" || success == "true") &&
		(errorValue == "" || errorValue == "false" || errorValue == "null") && len(r.Errors) == 0 {
		return nil
	}
	message := strings.TrimSpace(r.Message)
	var actionError OGameError
	if len(r.Errors) > 0 {
		actionError = r.Errors[0]
	} else if errorValue != "" {
		_ = json.Unmarshal(r.Error, &actionError)
	}
	if message == "" {
		message = strings.TrimSpace(actionError.Message)
	}
	if message == "" {
		message = "discovery request was rejected or remains unconfirmed"
	}
	if actionError.Error != 0 {
		return fmt.Errorf("%s (code %d)", message, actionError.Error)
	}
	return errors.New(message)
}

func (b *OGame) dispatchDiscoveryFleet(celestialID ogame.CelestialID, coord ogame.Coordinate, wantFleet bool, options ...Option) (ogame.Fleet, error) {
	var zero ogame.Fleet
	options = append(append([]Option{}, options...), ChangePlanet(celestialID))
	galaxyPage, err := b.getGalaxyPage(coord.Galaxy, coord.System, options...)
	if err != nil {
		return zero, err
	}
	pageToken := strings.TrimSpace(galaxyPage.Token)
	if pageToken == "" {
		return zero, errors.New("discovery page did not contain an action token")
	}
	eligible := false
	for _, position := range galaxyPage.System.GalaxyContent {
		if position.Position != coord.Position {
			continue
		}
		for _, mission := range position.AvailableMissions {
			if mission.MissionType != ogame.SearchForLifeforms {
				continue
			}
			if mission.CanSend != true {
				if reason, ok := mission.CanSend.(string); ok && strings.TrimSpace(reason) != "" {
					return zero, fmt.Errorf("can't send discovery mission: %s", reason)
				}
				return zero, errors.New("can't send discovery mission")
			}
			eligible = true
		}
	}
	if !eligible {
		return zero, errors.New("discovery mission is not available for the target")
	}
	origin, err := b.getCachedCelestialByID(celestialID)
	if err != nil {
		return zero, err
	}
	// Snapshot all IDs: a previously visible fleet with different or incomplete
	// route metadata must never become proof of a new dispatch.
	readOptions := append(append([]Option{}, options...), SkipRetry, SkipCacheFullPage)
	snapshot, err := b.getDiscoveryFleetSnapshot(readOptions...)
	if err != nil {
		return zero, fmt.Errorf("cannot snapshot fleets before discovery: %w", err)
	}
	initialIDs := make(map[ogame.FleetID]struct{}, len(snapshot.fleets))
	for _, fleet := range snapshot.fleets {
		initialIDs[fleet.ID] = struct{}{}
	}
	// Never replay this POST, including on a lost response, HTTP error or busy
	// application response. Read retries cannot prove the action was not applied.
	sendOptions := append(append([]Option{}, options...), SkipRetry)
	res, err := b.postPageContent(url.Values{
		"page": {"ingame"}, "component": {"fleetdispatch"}, "action": {"sendDiscoveryFleet"}, "ajax": {"1"}, "asJson": {"1"},
	}, url.Values{
		"galaxy": {utils.FI64(coord.Galaxy)}, "system": {utils.FI64(coord.System)}, "position": {utils.FI64(coord.Position)}, "token": {pageToken},
	}, sendOptions...)
	var redirectErr *requestRedirectError
	if errors.As(err, &redirectErr) && redirectErr.normal {
		return b.confirmDiscoveryFleet(origin.GetCoordinate(), coord, initialIDs, readOptions...)
	}
	if err != nil {
		return zero, fmt.Errorf("discovery dispatch outcome is unconfirmed: %w", err)
	}
	if err := parseDiscoveryResponse(res); err != nil {
		return zero, err
	}
	if wantFleet {
		return b.confirmDiscoveryFleet(origin.GetCoordinate(), coord, initialIDs, readOptions...)
	}
	return zero, nil
}

func (b *OGame) confirmDiscoveryFleet(origin, destination ogame.Coordinate, initialIDs map[ogame.FleetID]struct{}, options ...Option) (ogame.Fleet, error) {
	var zero ogame.Fleet
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err := b.getDiscoveryFleetSnapshot(options...)
		if err != nil {
			lastErr = err
		} else {
			var found ogame.Fleet
			for _, fleet := range snapshot.fleets {
				_, existed := initialIDs[fleet.ID]
				if fleet.ID > 0 && !existed && snapshot.provenOutgoing[fleet.ID] && fleet.Mission == ogame.SearchForLifeforms && !fleet.ReturnFlight &&
					fleet.Origin.Equal(origin) && fleet.Destination.Equal(destination) {
					if found.ID != 0 {
						return zero, errors.New("discovery dispatch is ambiguous: multiple new matching fleets")
					}
					found = fleet
				}
			}
			if found.ID != 0 {
				return found, nil
			}
		}
		if attempt < 2 {
			if err := applyDelay(b, 300*time.Millisecond); err != nil {
				return zero, err
			}
		}
	}
	if lastErr != nil {
		return zero, fmt.Errorf("discovery dispatch is unconfirmed: %w", lastErr)
	}
	return zero, errors.New("discovery dispatch is unconfirmed: no proven new matching fleet")
}

type discoveryFleetSnapshot struct {
	fleets         []ogame.Fleet
	provenOutgoing map[ogame.FleetID]bool
}

func (b *OGame) getDiscoveryFleetSnapshot(options ...Option) (discoveryFleetSnapshot, error) {
	var snapshot discoveryFleetSnapshot
	options = append(append([]Option{}, options...), SkipRetry, SkipCacheFullPage)
	body, err := b.pageContentWithHTTPStatus(http.MethodGet, url.Values{"page": {"ingame"}, "component": {"movement"}}, nil, true, options...)
	if err != nil {
		return snapshot, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return snapshot, err
	}
	if b.extractor.ExtractBodyIDFromDoc(doc) != "movement" {
		return snapshot, errors.New("discovery movement view has an invalid page identity")
	}
	current := doc.Find("body span.fleetSlots > span.current")
	rows := doc.Find("body div.fleetDetails")
	count, err := strconv.ParseInt(strings.TrimSpace(current.Text()), 10, 64)
	if current.Length() != 1 || err != nil || count < 0 || count != int64(rows.Length()) {
		return snapshot, errors.New("discovery movement view is incomplete: occupied slots do not match fleet details")
	}
	// The shared extractor intentionally supports incomplete legacy pages. A
	// discovery watermark additionally requires every occupied fleet to have a
	// unique explicit positive ID; otherwise an existing fleet could look new.
	details := make(map[ogame.FleetID]*goquery.Selection, rows.Length())
	for _, row := range rows.EachIter() {
		links := row.Find("a.openCloseDetails")
		var id int64
		for _, link := range links.EachIter() {
			linkID, parseErr := strconv.ParseInt(link.AttrOr("data-mission-id", ""), 10, 64)
			if parseErr != nil || linkID <= 0 || (id != 0 && id != linkID) {
				return snapshot, errors.New("discovery movement view has an unknown or conflicting detail-link ID")
			}
			id = linkID
		}
		if id == 0 || details[ogame.FleetID(id)] != nil {
			return snapshot, errors.New("discovery movement view has an unknown or duplicate fleet ID")
		}
		details[ogame.FleetID(id)] = row
	}
	snapshot.fleets, err = b.extractor.ExtractFleetsFromDoc(doc)
	if err != nil {
		return snapshot, err
	}
	if len(snapshot.fleets) != rows.Length() {
		return snapshot, errors.New("discovery movement view has incomplete parsed fleets")
	}
	snapshot.provenOutgoing = make(map[ogame.FleetID]bool, len(snapshot.fleets))
	for _, fleet := range snapshot.fleets {
		row := details[fleet.ID]
		if row == nil {
			return snapshot, errors.New("discovery movement view has inconsistent fleet IDs")
		}
		// Absent/empty direction and missing figures may be defaulted by the
		// general parser. They cannot prove an outgoing discovery or its types.
		snapshot.provenOutgoing[fleet.ID] = row.AttrOr("data-return-flight", "") == "false" &&
			row.AttrOr("data-mission-type", "") == "18" &&
			discoveryRouteEvidence(row, "origin", fleet.Origin) && discoveryRouteEvidence(row, "destination", fleet.Destination)
	}
	return snapshot, nil
}

func discoveryRouteEvidence(row *goquery.Selection, side string, coordinate ogame.Coordinate) bool {
	links := row.Find("span." + side + "Coords a")
	figures := row.Find("span." + side + "Planet figure")
	if links.Length() != 1 || figures.Length() != 1 ||
		strings.TrimSpace(links.Text()) != fmt.Sprintf("[%d:%d:%d]", coordinate.Galaxy, coordinate.System, coordinate.Position) {
		return false
	}
	switch coordinate.Type {
	case ogame.PlanetType:
		return figures.HasClass("planet") && !figures.HasClass("moon") && !figures.HasClass("tf")
	case ogame.MoonType:
		return figures.HasClass("moon") && !figures.HasClass("planet") && !figures.HasClass("tf")
	default:
		return false
	}
}
