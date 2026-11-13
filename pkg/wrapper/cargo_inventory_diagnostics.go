package wrapper

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/alaingilbert/ogame/pkg/ogame"
	"os"
	"strconv"
	"time"
)

// cargoInventoryDiagnostic emits counters only; never cookies, page content or session URLs.
func (b *OGame) cargoInventoryDiagnostic(content []byte, requested, actual ogame.CelestialID, ships ogame.ShipsInfos, extractErr error) {
	if os.Getenv("TBOT_CARGO_DIAGNOSTICS") != "1" {
		return
	}
	counters := []any{}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(content))
	if err == nil {
		doc.Find("#technologies .technology[data-technology='202'] .amount[data-value]").Each(func(_ int, s *goquery.Selection) {
			raw, _ := s.Attr("data-value")
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				counters = append(counters, "invalid")
			} else {
				counters = append(counters, n)
			}
		})
	}
	status := "parsed"
	if extractErr != nil {
		status = "rejected"
	}
	record, _ := json.Marshal(map[string]any{"Utc": time.Now().UTC(), "Stage": "html-inventory", "RequestedOriginId": requested, "ReturnedOriginId": actual, "ContentSHA256": fmt.Sprintf("%x", sha256.Sum256(content)), "SmallCargoCounters": counters, "ParsedSmallCargo": ships.SmallCargo, "Status": status})
	b.logger.Printf("CargoInventoryDiagnostic %s", record)
}
