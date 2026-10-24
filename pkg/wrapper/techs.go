package wrapper

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/alaingilbert/ogame/pkg/parser"
)

var techsCurrentPageRgx = regexp.MustCompile(`\bcurrentPage\s*=\s*["']([^"']+)["']`)

// OGame 13 removed fetchTechs. Read the ordinary component pages instead of
// requiring the Commander-only empire view. These are internal requests: the
// public GetTechs call already holds its task-runner lock.
func (b *OGame) getTechsFromComponents(celestialID ogame.CelestialID) (ogame.Techs, error) {
	celestial, err := b.getCachedCelestial(celestialID)
	if celestialID <= 0 || err != nil {
		return ogame.Techs{}, ogame.ErrInvalidPlanetID
	}
	researchID := celestialID
	if celestial.GetType() == ogame.MoonType {
		// Research belongs to the account, but its page is a planet facility.
		for _, planet := range b.getCachedPlanets() {
			if planet.Moon != nil && planet.Moon.GetID() == celestialID {
				researchID = planet.GetID()
				break
			}
		}
	}
	var out ogame.Techs
	type component struct {
		component string
		id        ogame.CelestialID
		extract   func(*goquery.Document) error
	}
	steps := []component{
		{SuppliesPageName, celestialID, func(doc *goquery.Document) error {
			out.ResourcesBuildings, err = b.extractor.ExtractResourcesBuildingsFromDoc(doc)
			return err
		}},
		{FacilitiesPageName, celestialID, func(doc *goquery.Document) error {
			out.Facilities, err = b.extractor.ExtractFacilitiesFromDoc(doc)
			return err
		}},
		{ShipyardPageName, celestialID, func(doc *goquery.Document) error {
			out.ShipsInfos, err = b.extractor.ExtractShipsFromDoc(doc)
			return err
		}},
		{DefensesPageName, celestialID, func(doc *goquery.Document) error {
			out.DefensesInfos, err = b.extractor.ExtractDefenseFromDoc(doc)
			return err
		}},
		{ResearchPageName, researchID, func(doc *goquery.Document) error {
			out.Researches = b.extractor.ExtractResearchFromDoc(doc)
			return nil
		}},
	}
	if celestial.GetType() == ogame.PlanetType && b.extractor.GetLifeformEnabled() {
		steps = append(steps, component{LfBuildingsPageName, celestialID, func(doc *goquery.Document) error {
			out.LfBuildings, err = b.extractor.ExtractLfBuildingsFromDoc(doc)
			return err
		}}, component{LfResearchPageName, celestialID, func(doc *goquery.Document) error {
			out.LfResearches, err = b.extractor.ExtractLfResearchFromDoc(doc)
			return err
		}})
	}
	for _, step := range steps {
		by, err := b.getPage(step.component, ChangePlanet(step.id), SkipCacheFullPage)
		if err != nil {
			return ogame.Techs{}, err
		}
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(by))
		if err != nil {
			return ogame.Techs{}, err
		}
		actualID, err := b.extractor.ExtractPlanetIDFromDoc(doc)
		if err != nil || actualID != step.id {
			return ogame.Techs{}, ogame.ErrInvalidPlanetID
		}
		page := techsCurrentPageRgx.FindSubmatch(by)
		if len(page) != 2 || string(page[1]) != step.component {
			return ogame.Techs{}, fmt.Errorf("unexpected %s page while fetching techs: %w", step.component, parser.ErrParsePageType)
		}
		if err := step.extract(doc); err != nil {
			return ogame.Techs{}, err
		}
	}
	return out, nil
}
