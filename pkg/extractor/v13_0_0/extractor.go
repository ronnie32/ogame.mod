package v13_0_0

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/extractor/v12_0_0"
	"github.com/alaingilbert/ogame/pkg/ogame"
)

// Extractor ...
type Extractor struct {
	v12_0_0.Extractor
}

// NewExtractor ...
func NewExtractor() *Extractor {
	return &Extractor{}
}

// ExtractLfBonuses ...
func (e *Extractor) ExtractLfBonuses(pageHTML []byte) (ogame.LfBonuses, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageHTML))
	if err != nil {
		return ogame.LfBonuses{}, err
	}
	return e.ExtractLfBonusesFromDoc(doc)
}

// ExtractLfBonusesFromDoc ...
func (e *Extractor) ExtractLfBonusesFromDoc(doc *goquery.Document) (ogame.LfBonuses, error) {
	return extractLfBonusesFromDoc(doc)
}

// ExtractOfferOfTheDay ...
func (e *Extractor) ExtractOfferOfTheDay(pageHTML []byte) (int64, string, ogame.PlanetResources, ogame.Multiplier, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageHTML))
	if err != nil {
		return 0, "", nil, ogame.Multiplier{}, err
	}
	return extractOfferOfTheDayFromDoc(doc)
}

// ExtractCancelFleetToken ...
func (e *Extractor) ExtractCancelFleetToken(pageHTML []byte, fleetID ogame.FleetID) (string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageHTML))
	if err != nil {
		return "", err
	}
	return extractCancelFleetTokenFromDoc(doc, fleetID)
}

// ExtractShips reads inventory counters, not decorative icons or production totals.
func (e *Extractor) ExtractShips(pageHTML []byte) (ogame.ShipsInfos, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(pageHTML))
	if err != nil {
		return ogame.ShipsInfos{}, err
	}
	return e.ExtractShipsFromDoc(doc)
}

// ExtractShipsFromDoc rejects incomplete inventory instead of inventing zero stock.
func (e *Extractor) ExtractShipsFromDoc(doc *goquery.Document) (ogame.ShipsInfos, error) {
	ships := ogame.ShipsInfos{}
	for _, id := range []ogame.ID{202, 203, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 217, 218, 219} {
		rows := doc.Find(fmt.Sprintf("#technologies .technology[data-technology='%d']", id))
		// Satellites and crawlers are not available on moons.
		if rows.Length() == 0 && (id == 212 || id == 217) {
			continue
		}
		if rows.Length() != 1 {
			return ogame.ShipsInfos{}, fmt.Errorf("invalid ship inventory: expected one technology row for %d, got %d", id, rows.Length())
		}
		counters := rows.Find(".amount[data-value]")
		if counters.Length() != 1 {
			return ogame.ShipsInfos{}, fmt.Errorf("invalid ship inventory: expected one stock counter for %d, got %d", id, counters.Length())
		}
		raw, _ := counters.Attr("data-value")
		amount, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || amount < 0 {
			return ogame.ShipsInfos{}, fmt.Errorf("invalid ship inventory: invalid stock counter for %d: %q", id, raw)
		}
		ships.Set(id, amount)
	}
	return ships, nil
}
