package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/alaingilbert/ogame/pkg/simulator"
	"github.com/labstack/echo/v4"
)

var combatSimulationSlots = make(chan struct{}, 2)

// CombatSimulationHandler is independent of login state and never contacts the game.
func CombatSimulationHandler(c echo.Context) error {
	select {
	case combatSimulationSlots <- struct{}{}:
		defer func() { <-combatSimulationSlots }()
	default:
		return c.JSON(http.StatusTooManyRequests, ErrorResp(429, "combat simulator is busy"))
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 256*1024)
	var request simulator.CombatRequest
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, err.Error()))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return c.JSON(http.StatusBadRequest, ErrorResp(400, "expected exactly one combat request"))
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	result, err := simulator.RunCombat(ctx, request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return c.JSON(http.StatusGatewayTimeout, ErrorResp(504, "combat simulation deadline exceeded"))
		}
		return c.JSON(http.StatusUnprocessableEntity, ErrorResp(422, err.Error()))
	}
	return c.JSON(http.StatusOK, SuccessResp(result))
}
