package v11_13_0

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFleetBackInModernTimer(t *testing.T) {
	page, err := os.ReadFile("../../../samples/v11.13.0/en/movement2.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"getElementByIdWithCache", "getElementById"} {
		t.Run(fn, func(t *testing.T) {
			ext := NewExtractor()
			ext.SetLocation(time.UTC)
			html := strings.ReplaceAll(string(page), "getElementByIdWithCache", fn)
			fleets, err := ext.ExtractFleets([]byte(html))
			if err != nil || len(fleets) != 1 {
				t.Fatalf("fleets=%v err=%v", fleets, err)
			}
			if fleets[0].BackIn != 3908 {
				t.Errorf("BackIn=%d want3908", fleets[0].BackIn)
			}
		})
	}
}
