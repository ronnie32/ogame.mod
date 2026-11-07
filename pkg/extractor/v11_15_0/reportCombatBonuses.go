package v11_15_0

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"golang.org/x/net/html"
)

func reportClassKnown(raw string) bool {
	var value struct{ ID *int }
	return json.Unmarshal([]byte(raw), &value) == nil && value.ID != nil && *value.ID >= 0 && *value.ID <= 3
}

// A malformed visible fleet must never be interpreted as an empty fleet.
func validateReportUnits(raw string, ships bool) error {
	if strings.TrimSpace(raw) == "[]" {
		return nil
	} // Native empty collection encoding.
	var quantities map[string]*int64
	if err := json.Unmarshal([]byte(raw), &quantities); err != nil || quantities == nil {
		return fmt.Errorf("invalid visible espionage unit data")
	}
	for key, amount := range quantities {
		number, err := strconv.ParseInt(key, 10, 64)
		id := ogame.ID(number)
		valid := ships && id.IsShip() || !ships && (id >= 401 && id <= 408 || id == 502 || id == 503)
		if err != nil || !valid || amount == nil || *amount < 0 {
			return fmt.Errorf("invalid or unsupported espionage unit %q", key)
		}
	}
	return nil
}

// Read only the aggregate headings. Descendant rows describe individual planets
// and technologies and must never overwrite the global total.
func extractReportCombatBonuses(doc *goquery.Document) *ogame.ReportCombatBonuses {
	scope := doc.Find("div.detail_msg").First().Find(".lfBonusReportWrapper")
	if scope.Length() != 1 {
		return nil
	}
	result := &ogame.ReportCombatBonuses{Source: "EspionageReportTable", Units: map[ogame.ID]ogame.ReportCombatUnitBonus{}}
	seen := map[ogame.ID]bool{}
	for _, category := range []struct {
		key, prefix string
		defense     bool
	}{
		{"ships", "", false}, {"categoryShips", "subcategoryShips", false},
		{"defenses", "", true}, {"categoryDefenses", "subcategoryDefenses", true},
	} {
		section := scope.Find(`bonus-item-content[data-toggable-target="` + category.key + `"]`)
		headers := section.ChildrenFiltered("bonus-item-content-holder").ChildrenFiltered("bonus-items")
		if headers.Length() != 1 {
			continue
		}
		columns := headers.ChildrenFiltered("bonus-item")
		if columns.Length() < 3 || columns.Eq(0).Find("armor-icon").Length() != 1 ||
			columns.Eq(1).Find("shield-icon").Length() != 1 || columns.Eq(2).Find("weapon-icon").Length() != 1 {
			continue
		}
		for _, heading := range section.ChildrenFiltered("bonus-item-content-holder").ChildrenFiltered("inner-bonus-item-heading[data-toggable]").EachIter() {
			key := strings.TrimPrefix(heading.AttrOr("data-toggable", ""), category.prefix)
			number, err := strconv.ParseInt(key, 10, 64)
			id := ogame.ID(number)
			if err != nil || (!category.defense && !id.IsShip()) || (category.defense && (id < 401 || id > 408)) {
				continue
			}
			if seen[id] {
				delete(result.Units, id)
				continue
			}
			seen[id] = true
			rows := heading.ChildrenFiltered("bonus-items")
			cells := rows.ChildrenFiltered("bonus-item")
			if rows.Length() != 1 || (cells.Length() != 6 && !(category.defense && cells.Length() == 3)) {
				continue
			}
			values := [3]*float64{}
			for i := range values {
				text := cells.Eq(i).Contents().FilterFunction(func(_ int, s *goquery.Selection) bool { return s.Nodes[0].Type == html.TextNode }).Text()
				values[i] = reportPercentage(text)
			}
			result.Units[id] = ogame.ReportCombatUnitBonus{StructuralIntegrity: values[0], ShieldPower: values[1], WeaponPower: values[2]}
		}
	}
	return result
}

var reportPercentPattern = regexp.MustCompile(`^\+?\d+(?:[.,]\d+)?%$`)

func reportPercentage(text string) *float64 {
	text = strings.TrimSpace(text)
	value := 0.0
	if text == "-" {
		return &value
	}
	if !reportPercentPattern.MatchString(text) {
		return nil
	}
	parsed, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSuffix(text, "%"), ",", "."), 64)
	if err != nil || parsed < 0 || parsed > 1000 {
		return nil
	}
	value = parsed / 100
	return &value
}
