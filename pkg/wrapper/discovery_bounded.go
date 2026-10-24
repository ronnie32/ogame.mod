package wrapper

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/alaingilbert/ogame/pkg/ogame"
	"github.com/labstack/echo/v4"
)

// SystemDiscoveryResult preserves proven dispatches if a later target becomes
// unavailable or its dispatch cannot be confirmed. An incomplete result must
// never be automatically replayed: the failed action might have been applied.
type SystemDiscoveryResult struct {
	Coordinates []ogame.Coordinate `json:"coordinates"`
	Complete    bool               `json:"complete"`
	Error       string             `json:"error,omitempty"`
}

func validateDiscoveryReservation(celestialID ogame.CelestialID, galaxy, system, maxFleets int64, positions []int64) error {
	if celestialID <= 0 || galaxy <= 0 || system <= 0 {
		return errors.New("positive origin, galaxy and system are required")
	}
	if maxFleets < 1 || maxFleets > 15 || len(positions) < 1 || int64(len(positions)) > maxFleets {
		return errors.New("maxFleets must be within [1, 15] and cover every reserved position")
	}
	seen := make(map[int64]bool, len(positions))
	for _, position := range positions {
		if position < 1 || position > 15 || seen[position] {
			return errors.New("positions must contain unique planet positions within [1, 15]")
		}
		seen[position] = true
	}
	return nil
}

func (b *OGame) sendSystemDiscoveryFleetBounded(celestialID ogame.CelestialID, galaxy, system, maxFleets int64, positions []int64, options ...Option) (SystemDiscoveryResult, error) {
	result := SystemDiscoveryResult{Coordinates: make([]ogame.Coordinate, 0, len(positions))}
	if err := validateDiscoveryReservation(celestialID, galaxy, system, maxFleets, positions); err != nil {
		return result, err
	}
	// The native system action has no supported server-side count/target limit.
	// A fresh eligible count can change before that action executes. Dispatching
	// only explicit targets enforces the caller's reservation in that race.
	b.discoveryDispatchMu.Lock()
	defer b.discoveryDispatchMu.Unlock()
	for _, position := range positions {
		target := ogame.NewPlanetCoordinate(galaxy, system, position)
		fleet, err := b.dispatchDiscoveryFleet(celestialID, target, true, options...)
		if err != nil {
			result.Error = fmt.Sprintf("discovery at [%d:%d:%d] is incomplete: %s", galaxy, system, position, err)
			return result, nil
		}
		result.Coordinates = append(result.Coordinates, fleet.Destination)
	}
	result.Complete = true
	return result, nil
}

// SendSystemDiscoveryBoundedHandler accepts explicit positions and a positive
// reserved fleet count. Missing bounds fail before any game request. Runtime
// failures return HTTP200 with complete=false and any earlier proven coordinates.
func SendSystemDiscoveryBoundedHandler(c echo.Context) error {
	planetID, err := strconv.ParseInt(c.Param("planetID"), 10, 64)
	if err != nil || planetID <= 0 {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, "invalid planet id"))
	}
	if err := c.Request().ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, "invalid form"))
	}
	form := c.Request().PostForm
	for _, name := range []string{"galaxy", "system", "maxFleets", "positions"} {
		if len(form[name]) != 1 || strings.TrimSpace(form.Get(name)) == "" {
			return c.JSON(http.StatusBadRequest, ErrorResp(400, "galaxy, system, maxFleets and positions are required once"))
		}
	}
	galaxy, galaxyErr := strconv.ParseInt(form.Get("galaxy"), 10, 64)
	system, systemErr := strconv.ParseInt(form.Get("system"), 10, 64)
	maxFleets, maxErr := strconv.ParseInt(form.Get("maxFleets"), 10, 64)
	if galaxyErr != nil || systemErr != nil || maxErr != nil {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, "invalid system discovery bounds"))
	}
	positions := make([]int64, 0)
	for _, raw := range strings.Split(form.Get("positions"), ",") {
		position, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResp(400, "invalid reserved position"))
		}
		positions = append(positions, position)
	}
	if err := validateDiscoveryReservation(ogame.CelestialID(planetID), galaxy, system, maxFleets, positions); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, err.Error()))
	}
	bot := c.Get("bot").(*OGame)
	result, err := bot.SendSystemDiscoveryFleetBounded(ogame.CelestialID(planetID), galaxy, system, maxFleets, positions)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, err.Error()))
	}
	return c.JSON(http.StatusOK, SuccessResp(result))
}
