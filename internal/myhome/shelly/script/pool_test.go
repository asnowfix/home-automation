package script

import (
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/schedule"
)

// poolPumpJob builds a schedule.Job shaped like what Schedule.List actually
// returns for a pool-pump schedule: a single script.eval call whose params
// carry "id" as a float64 (JSON-decoded numbers are always float64) and
// "code" containing the handler call, matching preservedScheduleState's and
// isPoolPumpScheduleCode's parsing.
func poolPumpJob(id uint32, scriptId int, enable bool, timespec string, code string) schedule.Job {
	return schedule.Job{
		JobId: schedule.JobId{Id: id},
		JobSpec: schedule.JobSpec{
			Enable:   enable,
			Timespec: timespec,
			Calls: []schedule.JobCall{{
				Method: "script.eval",
				Params: map[string]interface{}{
					"id":   float64(scriptId),
					"code": code,
				},
			}},
		},
	}
}

func TestGetDesiredSchedules(t *testing.T) {
	defs := getDesiredSchedules(7)
	if len(defs) != 5 {
		t.Fatalf("expected 5 schedule definitions, got %d", len(defs))
	}

	wantCodes := []string{
		"handleDailyCheck()",
		"handleMorningStart()",
		"handleEveningStop()",
		"handleNightStart()",
		"handleNightStop()",
	}
	for i, want := range wantCodes {
		if defs[i].Code != want {
			t.Errorf("defs[%d].Code: expected %q, got %q", i, want, defs[i].Code)
		}
	}

	// Morning start and evening stop ship disabled (winter mode); the rest enabled.
	wantEnabled := []bool{true, false, false, true, true}
	for i, want := range wantEnabled {
		if defs[i].Enable != want {
			t.Errorf("defs[%d].Enable: expected %v, got %v", i, want, defs[i].Enable)
		}
	}
}

// TestBuildJobSpec_WrapsCode is the #528 regression test: buildJobSpec() must
// not write def.Code bare into the job's script.eval params. An unwrapped
// eval means an uncaught throw inside the handler kills the whole script
// (AGENTS.md kill list rule 1) -- exactly what happened via #509/#530 once
// pool-pump.js's own wrapScheduleCall() became dead code and was removed in
// #526.
func TestBuildJobSpec_WrapsCode(t *testing.T) {
	def := scheduleDefinition{Enable: true, Timespec: "@sunrise * * *", Code: "handleDailyCheck()"}
	spec := buildJobSpec(42, def)

	if spec.Enable != true || spec.Timespec != "@sunrise * * *" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if len(spec.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(spec.Calls))
	}
	call := spec.Calls[0]
	if call.Method != "script.eval" {
		t.Errorf("expected method 'script.eval', got %q", call.Method)
	}
	params, ok := call.Params.(map[string]interface{})
	if !ok {
		t.Fatalf("expected params to be a map, got %T", call.Params)
	}
	if params["id"] != 42 {
		t.Errorf("expected id 42, got %v", params["id"])
	}

	code, ok := params["code"].(string)
	if !ok {
		t.Fatalf("expected code to be a string, got %T", params["code"])
	}

	// The bare handler call must not appear unwrapped -- it must be wrapped
	// in try/catch, and the code must not be the bare handler call itself.
	if code == def.Code {
		t.Fatalf("code was written bare (unwrapped): %q -- an uncaught throw in this eval "+
			"would kill the whole script (AGENTS.md kill list rule 1)", code)
	}
	if !strings.Contains(code, "try{"+def.Code) && !strings.Contains(code, "try {"+def.Code) {
		t.Errorf("expected code to wrap %q in a try block, got %q", def.Code, code)
	}
	if !strings.Contains(code, "catch") {
		t.Errorf("expected code to contain a catch block, got %q", code)
	}
	// The catch binding must be referenced -- an empty "catch (e) {}" is
	// reduced by the minifier to "catch {}", a syntax error on Espruino
	// (AGENTS.md kill list rule 5).
	if strings.Contains(code, "catch(e){}") || strings.Contains(code, "catch (e) {}") {
		t.Errorf("catch block does not reference its binding, would minify to a syntax error: %q", code)
	}

	// The containment matching the JS side relies on (indexOf('handleDailyCheck()')) must
	// still find the handler name inside the wrapped code.
	if !strings.Contains(code, def.Code) {
		t.Errorf("expected wrapped code to still contain the bare handler call %q for the JS "+
			"side's containment matching, got %q", def.Code, code)
	}
}

// TestIsPoolPumpScheduleCode_MatchesBareAndWrapped is the #528 regression
// test for reconcileSchedules()'s delete pass: it must recognize a
// pool-pump job's code whether it is bare (a device provisioned before
// #528) or wrapped (one provisioned after it), or a second provisioning run
// would never find and delete its own previously-created jobs and would
// pile up duplicates instead of replacing them.
func TestIsPoolPumpScheduleCode_MatchesBareAndWrapped(t *testing.T) {
	for _, name := range poolPumpScheduleHandlerNames() {
		if !isPoolPumpScheduleCode(name) {
			t.Errorf("isPoolPumpScheduleCode(%q) (bare) = false, want true", name)
		}
		wrapped := wrapScheduleJobCode(name)
		if !isPoolPumpScheduleCode(wrapped) {
			t.Errorf("isPoolPumpScheduleCode(%q) (wrapped) = false, want true", wrapped)
		}
	}

	for _, code := range []string{"", "someOtherScriptsHandler()", "(function(){try{someOtherScriptsHandler()}catch(e){if(e&&false){}}})()"} {
		if isPoolPumpScheduleCode(code) {
			t.Errorf("isPoolPumpScheduleCode(%q) = true, want false", code)
		}
	}
}

// TestScheduleHandlerNamesDontCollide is the #480 no-collision invariant
// (see internal/shelly/scripts/schedule_wrap_test.go), re-checked here
// against its actual authority: getDesiredSchedules() is now what decides
// which five handler names identify a pool-pump schedule (#524 moved
// schedule creation from pool-pump.js's own wrapScheduleCall() call sites to
// Go), so this is where the invariant has to hold. The JS side's
// containment matching (onWindowJobs()/updateScheduleMode()/
// verifySchedules() in pool-pump.js) only identifies the right job if no
// handler name is a substring of another, and if wrapScheduleJobCode()'s own
// boilerplate never happens to contain a handler name.
func TestScheduleHandlerNamesDontCollide(t *testing.T) {
	names := poolPumpScheduleHandlerNames()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 distinct handler names to make this check meaningful, got %d (%v)",
			len(names), names)
	}

	for i, a := range names {
		for j, b := range names {
			if i == j {
				continue
			}
			if strings.Contains(a, b) {
				t.Errorf("handler name %q contains %q as a substring -- containment-based "+
					"Schedule.List matching cannot tell these two handlers' jobs apart", a, b)
			}
		}
		if wrapped := wrapScheduleJobCode(""); strings.Contains(wrapped, a) {
			t.Errorf("wrapScheduleJobCode()'s boilerplate itself contains handler name %q -- "+
				"every job's code field would match this handler regardless of which handler "+
				"it actually calls", a)
		}
	}
}

func TestParseSpeedMappings(t *testing.T) {
	eco, mid, high := "3", "5", "9"

	tests := []struct {
		name           string
		eco, mid, high *string
		want           speedMappings
	}{
		{"all missing: defaults", nil, nil, nil, speedMappings{Eco: 0, Mid: 1, High: 2}},
		{"all present", &eco, &mid, &high, speedMappings{Eco: 3, Mid: 5, High: 9}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSpeedMappings(tt.eco, tt.mid, tt.high)
			if *got != tt.want {
				t.Errorf("expected %+v, got %+v", tt.want, *got)
			}
		})
	}

	t.Run("unparseable value keeps default", func(t *testing.T) {
		bad := "not-a-number"
		got := parseSpeedMappings(&bad, nil, nil)
		if got.Eco != 0 {
			t.Errorf("expected Eco to keep default 0, got %d", got.Eco)
		}
		if got.Mid != 1 || got.High != 2 {
			t.Errorf("expected Mid/High defaults preserved, got %+v", got)
		}
	})
}

func TestApplyEnvironmentKVSValues(t *testing.T) {
	t.Run("empty raw leaves env untouched", func(t *testing.T) {
		env := &Environment{PoolVolume: 99}
		applyEnvironmentKVSValues(env, map[string]string{})
		if env.PoolVolume != 99 {
			t.Errorf("expected PoolVolume to remain 99, got %d", env.PoolVolume)
		}
		if env.EcoSpeed != nil {
			t.Errorf("expected EcoSpeed to remain nil, got %v", *env.EcoSpeed)
		}
	})

	t.Run("all fields applied", func(t *testing.T) {
		env := &Environment{}
		raw := map[string]string{
			"temperature_threshold": "18.5",
			"eco_speed":             "1",
			"mid_speed":             "2",
			"high_speed":            "3",
			"pool_volume":           "50000",
			"turnover":              "4",
			"max_flow_rate":         "12",
			"max_rpm":               "2900",
			"eco_rpm":               "1200",
			"mid_rpm":               "2000",
			"high_rpm":              "2900",
			"max_temp":              "32.5",
			"mqtt_topic_prefix":     "pool/pump",
		}
		applyEnvironmentKVSValues(env, raw)

		if env.TemperatureThreshold != 18.5 {
			t.Errorf("TemperatureThreshold: expected 18.5, got %v", env.TemperatureThreshold)
		}
		if env.EcoSpeed == nil || *env.EcoSpeed != 1 {
			t.Errorf("EcoSpeed: expected 1, got %v", env.EcoSpeed)
		}
		if env.MidSpeed == nil || *env.MidSpeed != 2 {
			t.Errorf("MidSpeed: expected 2, got %v", env.MidSpeed)
		}
		if env.HighSpeed == nil || *env.HighSpeed != 3 {
			t.Errorf("HighSpeed: expected 3, got %v", env.HighSpeed)
		}
		if env.PoolVolume != 50000 {
			t.Errorf("PoolVolume: expected 50000, got %d", env.PoolVolume)
		}
		if env.Turnover != 4 {
			t.Errorf("Turnover: expected 4, got %d", env.Turnover)
		}
		if env.MaxFlowRate != 12 {
			t.Errorf("MaxFlowRate: expected 12, got %d", env.MaxFlowRate)
		}
		if env.MaxRpm != 2900 {
			t.Errorf("MaxRpm: expected 2900, got %d", env.MaxRpm)
		}
		if env.EcoRpm != 1200 || env.MidRpm != 2000 || env.HighRpm != 2900 {
			t.Errorf("Eco/Mid/HighRpm: expected 1200/2000/2900, got %d/%d/%d", env.EcoRpm, env.MidRpm, env.HighRpm)
		}
		if env.MaxTemp != 32.5 {
			t.Errorf("MaxTemp: expected 32.5, got %v", env.MaxTemp)
		}
		if env.MqttTopicPrefix != "pool/pump" {
			t.Errorf("MqttTopicPrefix: expected 'pool/pump', got %q", env.MqttTopicPrefix)
		}
	})

	t.Run("unparseable value leaves field untouched", func(t *testing.T) {
		env := &Environment{MaxRpm: 42}
		applyEnvironmentKVSValues(env, map[string]string{"max_rpm": "not-a-number"})
		if env.MaxRpm != 42 {
			t.Errorf("expected MaxRpm to remain 42 on parse failure, got %d", env.MaxRpm)
		}
	})
}

// TestPreservedScheduleState_CapturesExistingEnableAndTimespec is the #589
// part-2 regression test: preservedScheduleState must report each existing
// pool-pump job's on-device Enable/Timespec, keyed by handler name, so
// reconcileSchedules can carry it forward instead of overwriting it with
// getDesiredSchedules()'s fresh-install defaults.
func TestPreservedScheduleState_CapturesExistingEnableAndTimespec(t *testing.T) {
	scriptId := 7
	existing := []schedule.Job{
		poolPumpJob(1, scriptId, true, "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleDailyCheck()"),
		poolPumpJob(2, scriptId, true, "0 3 13 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleMorningStart()"),
		poolPumpJob(3, scriptId, true, "0 57 16 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleEveningStop()"),
		poolPumpJob(4, scriptId, false, "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleNightStart()"),
		poolPumpJob(5, scriptId, false, "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleNightStop()"),
		// A different script's job -- must not be picked up.
		poolPumpJob(6, scriptId+1, true, "0 0 12 * * *", "handleMorningStart()"),
		// A non-pool-pump job -- must not be picked up.
		{JobId: schedule.JobId{Id: 7}, JobSpec: schedule.JobSpec{Enable: true, Timespec: "0 0 3 * * *", Calls: []schedule.JobCall{{Method: "Shelly.Update"}}}},
	}

	preserved := preservedScheduleState(existing, scriptId)

	want := map[string]scheduleState{
		"handleDailyCheck()":   {Enable: true, Timespec: "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT"},
		"handleMorningStart()": {Enable: true, Timespec: "0 3 13 * * SUN,MON,TUE,WED,THU,FRI,SAT"},
		"handleEveningStop()":  {Enable: true, Timespec: "0 57 16 * * SUN,MON,TUE,WED,THU,FRI,SAT"},
		"handleNightStart()":   {Enable: false, Timespec: "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT"},
		"handleNightStop()":    {Enable: false, Timespec: "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT"},
	}
	if len(preserved) != len(want) {
		t.Fatalf("expected %d preserved handlers, got %d: %+v", len(want), len(preserved), preserved)
	}
	for name, w := range want {
		got, ok := preserved[name]
		if !ok {
			t.Errorf("expected preserved state for %q, found none", name)
			continue
		}
		if got != w {
			t.Errorf("preserved[%q] = %+v, want %+v", name, got, w)
		}
	}
}

// TestReconcile_PreservesEnableAndTimespec_OnAlreadyProvisionedDevice
// reproduces the exact production incident from #589 part 2: an
// operational pump has morning/evening jobs enabled (summer mode) and
// night jobs disabled -- the opposite of getDesiredSchedules()'s
// fresh-install winter defaults. Reconciling must not flip them; only the
// job code (e.g. the #528 try/catch wrapping) should change.
func TestReconcile_PreservesEnableAndTimespec_OnAlreadyProvisionedDevice(t *testing.T) {
	scriptId := 3
	// State observed on filtration-hiver, 2026-09-02, per #589's table.
	existing := []schedule.Job{
		poolPumpJob(2, scriptId, true, "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleDailyCheck()"),
		poolPumpJob(3, scriptId, true, "0 3 13 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleMorningStart()"),
		poolPumpJob(4, scriptId, true, "0 57 16 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleEveningStop()"),
		poolPumpJob(5, scriptId, false, "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleNightStart()"),
		poolPumpJob(6, scriptId, false, "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT", "handleNightStop()"),
	}

	// This is the exact merge reconcileSchedules performs before its
	// delete/create passes -- exercised directly here since reconcile
	// itself talks to a live device via sd.CallE, which this package has
	// no fake/mock transport for.
	desired := getDesiredSchedules(scriptId)
	preserved := preservedScheduleState(existing, scriptId)
	for i := range desired {
		if state, ok := preserved[desired[i].Code]; ok {
			desired[i].Enable = state.Enable
			desired[i].Timespec = state.Timespec
		}
	}

	byCode := make(map[string]scheduleDefinition, len(desired))
	for _, def := range desired {
		byCode[def.Code] = def
	}

	// Morning start and evening stop must stay enabled -- getDesiredSchedules()
	// alone would disable them.
	if !byCode["handleMorningStart()"].Enable {
		t.Errorf("handleMorningStart() must stay enabled (operational summer state), reconcile would disable it")
	}
	if byCode["handleMorningStart()"].Timespec != "0 3 13 * * SUN,MON,TUE,WED,THU,FRI,SAT" {
		t.Errorf("handleMorningStart() timespec changed: got %q", byCode["handleMorningStart()"].Timespec)
	}
	if !byCode["handleEveningStop()"].Enable {
		t.Errorf("handleEveningStop() must stay enabled (operational summer state), reconcile would disable it")
	}

	// Night jobs must stay disabled -- getDesiredSchedules() alone would
	// enable them, arming an unauthorized 23:15 pump run (#589).
	if byCode["handleNightStart()"].Enable {
		t.Errorf("handleNightStart() must stay disabled, reconcile would arm an unauthorized 23:15 run")
	}
	if byCode["handleNightStop()"].Enable {
		t.Errorf("handleNightStop() must stay disabled")
	}
}

// TestReconcile_NewHandler_GetsDesiredDefault covers the other side of
// #589 part 2: a handler with no existing job on the device (e.g. a
// genuinely new device, or a schedule that was manually deleted) must
// still get getDesiredSchedules()'s default, not an empty/zero state.
func TestReconcile_NewHandler_GetsDesiredDefault(t *testing.T) {
	scriptId := 9
	desired := getDesiredSchedules(scriptId)
	preserved := preservedScheduleState(nil, scriptId) // no existing jobs at all
	for i := range desired {
		if state, ok := preserved[desired[i].Code]; ok {
			desired[i].Enable = state.Enable
			desired[i].Timespec = state.Timespec
		}
	}

	want := getDesiredSchedules(scriptId)
	for i := range desired {
		if desired[i] != want[i] {
			t.Errorf("desired[%d] = %+v, want unmodified default %+v", i, desired[i], want[i])
		}
	}
}
