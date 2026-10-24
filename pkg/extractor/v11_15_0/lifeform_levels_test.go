package v11_15_0

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestExtractLifeformExperienceLevels(t *testing.T) {
	fixture, err := os.ReadFile("../../../samples/v11.15.4/en/lfbonuses.html")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		page string
		want map[string]int64
	}{
		{"existing fixture", string(fixture), map[string]int64{"1": 4, "3": 4, "4": 0}},
		{"older page", `<html></html>`, map[string]int64{}},
		{"valid and invalid levels", `<lifeform-level-bonuses>
		<lifeform-item><div class="lifeform-item-icon lifeform2"></div><div class="currentlevel"><strong> 17 </strong></div></lifeform-item>
		<lifeform-item><div class="lifeform-item-icon lifeform1"></div><div class="currentlevel"><strong>bad</strong></div></lifeform-item>
		<lifeform-item><div class="lifeform-item-icon lifeform3"></div><div class="currentlevel"><strong>-1</strong></div></lifeform-item>
		<lifeform-item><div class="lifeform-item-icon lifeform5"></div><div class="currentlevel"><strong>9</strong></div></lifeform-item>
		<lifeform-item><div class="lifeform-item-icon otherlifeform4"></div><div class="currentlevel"><strong>8</strong></div></lifeform-item>
		</lifeform-level-bonuses>`, map[string]int64{"2": 17}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bonuses, err := NewExtractor().ExtractLfBonuses([]byte(tt.page))
			if err != nil {
				t.Fatal(err)
			}
			// Check the API representation, including allocation on older pages.
			encoded, err := json.Marshal(bonuses)
			if err != nil {
				t.Fatal(err)
			}
			var got struct{ LifeformLevels map[string]int64 }
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.LifeformLevels, tt.want) {
				t.Errorf("LifeformLevels = %#v, want %#v", got.LifeformLevels, tt.want)
			}
		})
	}
}
