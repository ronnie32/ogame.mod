package v13_0_0

import (
	"bytes"

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
	// Retain bonuses independent of the renamed categories, such as experience
	// levels, when the embedded extractor supports them.
	bonuses, err := e.Extractor.ExtractLfBonusesFromDoc(doc)
	if err != nil {
		return bonuses, err
	}
	extractLfBonusesFromDoc(doc, &bonuses)
	return bonuses, nil
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

func (e *Extractor) ExtractOfferOfTheDayFromDoc(doc *goquery.Document) (int64, string, ogame.PlanetResources, ogame.Multiplier, error) {
	return extractOfferOfTheDayFromDoc(doc)
}
