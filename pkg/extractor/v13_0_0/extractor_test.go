package v13_0_0

import (
	"github.com/alaingilbert/ogame/pkg/ogame"
	"testing"
)

func TestLifeformBonuses(t *testing.T) {
	ext := NewExtractor()
	html := `<bonus-item-content data-toggable-target="ships"><bonus-item-content-holder><inner-bonus-item-heading data-toggable="202"><bonus-items><bonus-item>-</bonus-item><bonus-item>1%</bonus-item><bonus-item>2%</bonus-item><bonus-item>3,04%</bonus-item><bonus-item>4.87%</bonus-item><bonus-item>-0.13%</bonus-item></bonus-items></inner-bonus-item-heading><bonus-item-content><inner-bonus-item-heading data-toggable="202"><bonus-items><bonus-item>99%</bonus-item></bonus-items></inner-bonus-item-heading></bonus-item-content></bonus-item-content-holder></bonus-item-content>
 <bonus-item-content data-toggable-target="costreduction"><bonus-item-content-holder><inner-bonus-item-heading data-toggable="114"><bonus-items><bonus-item>2%</bonus-item><bonus-item>1.11%</bonus-item></bonus-items></inner-bonus-item-heading></bonus-item-content-holder></bonus-item-content>
 <bonus-item-content data-toggable-target="expedition"><bonus-item-content-holder><inner-bonus-item-heading data-toggable="ResultBooster"><div class="subCategoryBonus">2.23%</div></inner-bonus-item-heading></bonus-item-content-holder></bonus-item-content>
 <bonus-item-content data-toggable-target="characterclasses"><bonus-item-content-holder><inner-bonus-item-heading data-toggable="603"><div class="subCategoryBonus">Total: 1.5%</div></inner-bonus-item-heading></bonus-item-content-holder></bonus-item-content>`
	b, err := ext.ExtractLfBonuses([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	ship := b.LfShipBonuses[ogame.SmallCargoID]
	if ship.Speed != 0.0304 || ship.CargoCapacity != 0.0487 || ship.FuelConsumption != -0.0013 || ship.StructuralIntegrity != 0 {
		t.Errorf("ship=%+v", ship)
	}
	if b.CostTimeBonuses[ogame.HyperspaceTechnologyID].Cost != 0.02 || b.CostTimeBonuses[ogame.HyperspaceTechnologyID].Duration != 0.0111 {
		t.Errorf("cost/time=%v", b.CostTimeBonuses)
	}
	if b.LfResourceBonuses.ResourcesExpedition != 0.0223 || b.CharacterClassesBonuses.Characterclasses3 != 0.015 {
		t.Errorf("bonuses=%+v", b)
	}
}
