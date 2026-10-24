package v11_15_0

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLifeformProductionBonuses(t *testing.T) {
	html, err := os.ReadFile("../../../samples/v11.15.4/en/lfbonuses.html")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewExtractor().ExtractLfBonuses(html)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the exported response so the initial test compiles before fields exist.
	raw, _ := json.Marshal(b.LfResourceBonuses)
	var values map[string]float64
	_ = json.Unmarshal(raw, &values)
	for k, want := range map[string]float64{"ResourcesMetalProduction": 0.0072, "ResourcesCrystalProduction": 0.0072, "ResourcesDeuteriumProduction": 0.016} {
		if values[k] != want {
			t.Errorf("%s=%v want%v", k, values[k], want)
		}
	}
}
