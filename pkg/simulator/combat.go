package simulator

// The bounded HTTP engine uses the game's unit catalogue, ordinary floating
// point combat stats (not the legacy packed bit fields), and a private RNG.
// Units destroyed during a round still fire; casualties are removed together.
import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/alaingilbert/ogame/pkg/ogame"
)

type CombatForce struct {
	Ships     ogame.ShipsInfos
	Defenses  ogame.DefensesInfos
	Weapons   int64
	Shielding int64
	Armour    int64
	// Fractions of base stats, in the same additive bracket as research.
	Bonuses     ogame.LfShipBonuses
	BonusMargin float64
}

type CombatRequest struct {
	Attacker  CombatForce
	Defender  CombatForce
	Runs      int
	Seed      *int64
	RapidFire bool
	// Only for explicitly replaying old captures. Live planning leaves this false.
	LegacyProbeStats bool
}

type CombatResult struct {
	Runs             int
	AttackerWins     int
	DefenderWins     int
	Draws            int
	MeanAttackerLoss float64
	MaxAttackerLoss  float64
	MinSurvivors     map[string]int64
	MeanRounds       float64
	MaxRounds        int
}

type combatStat struct {
	id                         ogame.ID
	name                       string
	weapon, shield, hull, cost float64
	rapid                      map[ogame.ID]int64
}
type combatShip struct {
	stat         *combatStat
	shield, hull float64
}

// Historical native captures contain probe weapon/shield values of 0.01.
// Current probes have zero weapon/shield stats; historical values are opt-in.
func BaseCombatStats(object ogame.DefenderObj, legacyProbes bool) (weapon, shield, hull float64) {
	base := ogame.Researches{}
	weapon = float64(object.GetWeaponPower(base))
	shield = float64(object.GetShieldPower(base))
	hull = float64(object.GetStructuralIntegrity(base)) / 10
	if legacyProbes && object.GetID() == ogame.EspionageProbeID {
		weapon, shield = 0.01, 0.01
	}
	return
}

func forceUnits(force CombatForce, attacking, legacyProbes bool) ([]combatShip, error) {
	if force.BonusMargin != 0 {
		return nil, errors.New("estimated combat margins are unsupported; provide reported bonuses")
	}
	if force.Weapons < 0 || force.Weapons > 100 || force.Shielding < 0 || force.Shielding > 100 || force.Armour < 0 || force.Armour > 100 ||
		math.IsNaN(force.BonusMargin) || math.IsInf(force.BonusMargin, 0) || force.BonusMargin < 0 || force.BonusMargin > 10 {
		return nil, errors.New("invalid combat technologies or bonus margin")
	}
	var objects []ogame.DefenderObj
	for _, ship := range ogame.Ships {
		objects = append(objects, ship)
	}
	for _, defense := range ogame.Defenses {
		objects = append(objects, defense)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].GetID() < objects[j].GetID() })
	units := make([]combatShip, 0)
	for _, object := range objects {
		id := object.GetID()
		amount := force.Ships.ByID(id)
		if id >= 400 {
			amount = force.Defenses.ByID(id)
		}
		if amount < 0 || amount > 50000 || int64(len(units))+amount > 50000 {
			return nil, errors.New("combat supports at most 50000 units per side; negative quantities are invalid")
		}
		if id == ogame.AntiBallisticMissilesID || id == ogame.InterplanetaryMissilesID {
			continue // Missiles do not participate in fleet combat.
		}
		if attacking && amount > 0 && (id >= 400 || id == ogame.SolarSatelliteID || id == ogame.CrawlerID) {
			return nil, errors.New("attacking force contains immobile units")
		}
		if amount == 0 {
			continue
		}
		bonus := force.Bonuses[id]
		for _, value := range []float64{bonus.WeaponPower, bonus.ShieldPower, bonus.StructuralIntegrity} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10 {
				return nil, errors.New("invalid lifeform combat bonus")
			}
		}
		price := object.GetPrice(1, ogame.LfBonuses{})
		weapon, shield, hull := BaseCombatStats(object, legacyProbes)
		stat := &combatStat{id: id, name: id.String(),
			weapon: weapon * (1 + float64(force.Weapons)*0.1 + bonus.WeaponPower),
			shield: shield * (1 + float64(force.Shielding)*0.1 + bonus.ShieldPower),
			hull:   hull * (1 + float64(force.Armour)*0.1 + bonus.StructuralIntegrity),
			cost:   float64(price.Metal + price.Crystal + price.Deuterium), rapid: object.GetRapidfireAgainst()}
		for count := int64(0); count < amount; count++ {
			units = append(units, combatShip{stat: stat, shield: stat.shield, hull: stat.hull})
		}
	}
	return units, nil
}

// RunCombat returns sample statistics, not a guarantee about a future battle.
// No size scaling or ratio-based approximation is used when a budget is exceeded.
func RunCombat(ctx context.Context, request CombatRequest) (CombatResult, error) {
	result := CombatResult{Runs: request.Runs, MinSurvivors: map[string]int64{}}
	if request.Runs < 1 || request.Runs > 512 {
		return result, errors.New("Runs must be between 1 and 512")
	}
	attacker, err := forceUnits(request.Attacker, true, request.LegacyProbeStats)
	if err != nil {
		return result, err
	}
	defender, err := forceUnits(request.Defender, false, request.LegacyProbeStats)
	if err != nil {
		return result, err
	}
	if len(attacker) == 0 {
		return result, errors.New("attacking force is empty")
	}
	var seed int64
	if request.Seed != nil {
		seed = *request.Seed
	} else {
		var data [8]byte
		if _, err = cryptorand.Read(data[:]); err != nil {
			return result, err
		}
		seed = int64(binary.LittleEndian.Uint64(data[:]))
	}
	rng := rand.New(rand.NewSource(seed))
	a := make([]combatShip, len(attacker))
	d := make([]combatShip, len(defender))
	shots := 0
	for run := 0; run < request.Runs; run++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		a = a[:len(attacker)]
		copy(a, attacker)
		d = d[:len(defender)]
		copy(d, defender)
		rounds := 0
		for rounds < 6 && len(a) > 0 && len(d) > 0 {
			rounds++
			if err := combatVolley(ctx, a, d, rng, request.RapidFire, &shots); err != nil {
				return result, err
			}
			if err := combatVolley(ctx, d, a, rng, request.RapidFire, &shots); err != nil {
				return result, err
			}
			a = survivingCombatUnits(a)
			d = survivingCombatUnits(d)
		}
		switch {
		case len(a) > 0 && len(d) == 0:
			result.AttackerWins++
		case len(d) > 0 && len(a) == 0:
			result.DefenderWins++
		default:
			result.Draws++
		}
		loss := 0.0
		for _, unit := range attacker {
			loss += unit.stat.cost
		}
		survivors := map[string]int64{}
		for _, unit := range a {
			loss -= unit.stat.cost
			survivors[unit.stat.name]++
		}
		result.MeanAttackerLoss += loss
		result.MaxAttackerLoss = math.Max(result.MaxAttackerLoss, loss)
		result.MeanRounds += float64(rounds)
		result.MaxRounds = max(result.MaxRounds, rounds)
		for _, unit := range attacker {
			name := unit.stat.name
			if run == 0 {
				result.MinSurvivors[name] = survivors[name]
			} else {
				result.MinSurvivors[name] = min(result.MinSurvivors[name], survivors[name])
			}
		}
	}
	result.MeanAttackerLoss /= float64(request.Runs)
	result.MeanRounds /= float64(request.Runs)
	return result, nil
}

func survivingCombatUnits(units []combatShip) []combatShip {
	alive := units[:0]
	for _, unit := range units {
		if unit.hull > 0 {
			unit.shield = unit.stat.shield
			alive = append(alive, unit)
		}
	}
	return alive
}

func combatVolley(ctx context.Context, firing, targets []combatShip, rng *rand.Rand, rapid bool, shots *int) error {
	for _, unit := range firing {
		if unit.stat.weapon == 0 {
			continue
		}
		for {
			*shots++
			if *shots > 80000000 {
				return fmt.Errorf("combat shot budget exceeded")
			}
			if *shots%2048 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			target := &targets[rng.Intn(len(targets))]
			if target.hull > 0 && unit.stat.weapon >= target.stat.shield*0.01 {
				damage := unit.stat.weapon
				if target.shield >= damage {
					target.shield -= damage
				} else {
					target.hull -= damage - target.shield
					target.shield = 0
				}
				if target.hull > 0 && target.hull < target.stat.hull*0.7 && rng.Float64() > target.hull/target.stat.hull {
					target.hull = 0
				}
			}
			rf := unit.stat.rapid[target.stat.id]
			if !rapid || rf <= 1 || rng.Int63n(rf) == 0 {
				break
			}
		}
	}
	return nil
}
