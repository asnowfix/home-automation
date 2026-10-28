package garden

// ZoneDefault holds initial per-zone configuration values.
type ZoneDefault struct {
	name         string
	appRateMmH   float64
	kc           float64
	triggerMm    float64
	maxMin       int
	fallbackMin  int
	group        string
	intervalDays int
	enabled      bool
}

// defaultZoneDefaults mirrors ZONE_DEFAULTS from garden.js.
//
// NOTE: unlike garden_defaults_generated.go, this is hand-maintained, not
// generated. Before #439 it lived inside tools/extract-garden-defaults'
// output template as a literal Go struct — i.e. it was never actually
// parsed from garden.js despite living in a "generated" file; it was
// hand-authored there and just re-copied verbatim on every generate run.
// Moving it to a plain checked-in file makes that honest: edit this file
// and internal/shelly/scripts/garden.js's ZONE_DEFAULTS together by hand.
//
// Grass zones (0,1): 192 mm/h = 2 pop-up heads × 96 mm/h each (measured: 8 mm/5 min).
// Massifs zone (2): drip pipe — update appRateMmH after measuring with catch-cups.
// massifs (zone 2) plant list — true mediterranean, low-water (rosemary/Romarin,
// society garlic/Tulbaghia, boxwood/Buis, NZ flax/Phormium, Abelia, feijoa) mixed
// with thirstier plants (lemon/Citronnier, orange/Oranger de Chine, bird-of-paradise/
// Strelitzia, Agapanthus, daylily/Hémérocalle, Carex/Laîche).
// group/intervalDays: zones sharing a group water on the same days, gated by the
// minimum interval (days) across the group's enabled members. Lawn fires together
// daily-eligible; massifs intervalDays=4 is the watering-cadence compromise between
// the two plant groups above (see docs/garden-sprinklers-plan.md §11 for rationale).
var defaultZoneDefaults = []ZoneDefault{
	{name: "pelouse-maison", appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8, group: "lawn", intervalDays: 1, enabled: true},
	{name: "pelouse-barriere", appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8, group: "lawn", intervalDays: 1, enabled: true},
	{name: "massifs", appRateMmH: 18.0, kc: 0.6, triggerMm: 8.0, maxMin: 30, fallbackMin: 15, group: "beds", intervalDays: 4, enabled: true},
}
