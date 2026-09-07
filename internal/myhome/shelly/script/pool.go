package script

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly"
	sinput "github.com/asnowfix/home-automation/pkg/shelly/input"
	"github.com/asnowfix/home-automation/pkg/shelly/kvs"
	"github.com/asnowfix/home-automation/pkg/shelly/schedule"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/asnowfix/home-automation/pkg/shelly/types"

	"github.com/go-logr/logr"
)

// PoolKVSKeys maps configuration fields to KVS keys. Generated from
// internal/shelly/scripts/pool-pump.schema.json by
// internal/myhome/shelly/script/generate.go (issue #439) — see
// pool_kvs_generated.go. The Shelly KVS key length limit is validated at
// generation time, not by hand-counted comments.

// PoolService handles pool pump operations
type PoolService struct {
	log      logr.Logger
	provider DeviceProvider
}

// NewPoolService creates a new pool service
func NewPoolService(log logr.Logger, provider DeviceProvider) *PoolService {
	return &PoolService{
		log:      log.WithName("PoolService"),
		provider: provider,
	}
}

// SetupOptions contains configuration for pool setup
type SetupOptions struct {
	DeviceIDs            []string // Device IDs to set up
	PreferredSpeed       string   // "eco", "day" or "max" ("max" is the only stage a Pro1 has)
	NightRunDurationMs   int
	EcoSpeed             int
	DaySpeed             int
	MaxSpeed             int
	TemperatureThreshold float64
	PoolVolume           int
	Turnover             int
	MaxFlowRate          int
	MaxRpm               int
	EcoRpm               int
	DayRpm               int
	MaxTemp              float64

	// Solar-driven hysteresis (#405)
	SolarEnabled         bool
	SolarStartThresholdW int
	SolarStopThresholdW  int
	SolarStartDelayMs    int
	SolarStopDelayMs     int
	SolarMinTurnover     int
	SolarMaxTurnover     int
	SolarStaleMs         int

	// Manual override (#476): how long a button press or an out-of-band
	// switch change holds the pump against the schedule/solar policy.
	OverrideMs int

	ForceUpload bool
	NoMinify    bool
}

// Setup configures the pool pump controller(s).
//
// One controller is wired to the pump at a time; several may be configured (a
// Pro3 kept as a spare for a Pro1, say), but they never coordinate — each acts
// solely on its own schedule. There is therefore no minimum device count and no
// election.
func (s *PoolService) Setup(ctx context.Context, opts SetupOptions) error {
	s.log.Info("Setting up pool pump", "devices", len(opts.DeviceIDs))

	if len(opts.DeviceIDs) < 1 {
		return fmt.Errorf("at least 1 device required, got %d", len(opts.DeviceIDs))
	}

	// Resolve all device IDs
	deviceInfos := make([]struct {
		InputID string
		ID      string
		SD      *shelly.Device
	}, 0, len(opts.DeviceIDs))

	for _, id := range opts.DeviceIDs {
		dev, err := s.provider.GetDeviceByAny(ctx, id)
		if err != nil {
			return fmt.Errorf("device not found: %s: %w", id, err)
		}
		sd, err := s.provider.GetShellyDevice(ctx, dev)
		if err != nil {
			return fmt.Errorf("failed to get shelly device %s: %w", id, err)
		}
		deviceInfos = append(deviceInfos, struct {
			InputID string
			ID      string
			SD      *shelly.Device
		}{InputID: id, ID: dev.Id(), SD: sd})
	}

	// Use MQTT channel for KVS operations
	via := types.ChannelMqtt

	// Setup each device with the same configuration
	for _, info := range deviceInfos {
		if err := s.setupDevice(ctx, via, info.SD, info.ID, opts); err != nil {
			return fmt.Errorf("failed to setup device %s: %w", info.ID, err)
		}
	}

	s.log.Info("Pool pump setup complete", "device_count", len(deviceInfos))
	return nil
}

func (s *PoolService) setupDevice(ctx context.Context, via types.Channel, sd *shelly.Device, deviceID string, opts SetupOptions) error {
	s.log.Info("Setting up device", "device", sd.Name(), "id", deviceID)

	// All devices get the same KVS configuration
	kvsConfig := map[string]string{
		PoolKVSKeys["enable_logging"]:        "true",
		PoolKVSKeys["mqtt_topic_prefix"]:     "pool/pump",
		PoolKVSKeys["preferred_speed"]:       opts.PreferredSpeed,
		PoolKVSKeys["eco_speed"]:             fmt.Sprintf("%d", opts.EcoSpeed),
		PoolKVSKeys["day_speed"]:             fmt.Sprintf("%d", opts.DaySpeed),
		PoolKVSKeys["max_speed"]:             fmt.Sprintf("%d", opts.MaxSpeed),
		PoolKVSKeys["night_run_duration_ms"]: fmt.Sprintf("%d", opts.NightRunDurationMs),
		PoolKVSKeys["temperature_threshold"]: fmt.Sprintf("%.1f", opts.TemperatureThreshold),
		PoolKVSKeys["pool_volume"]:           fmt.Sprintf("%d", opts.PoolVolume),
		PoolKVSKeys["turnover"]:              fmt.Sprintf("%d", opts.Turnover),
		PoolKVSKeys["max_flow_rate"]:         fmt.Sprintf("%d", opts.MaxFlowRate),
		PoolKVSKeys["max_rpm"]:               fmt.Sprintf("%d", opts.MaxRpm),
		PoolKVSKeys["eco_rpm"]:               fmt.Sprintf("%d", opts.EcoRpm),
		PoolKVSKeys["day_rpm"]:               fmt.Sprintf("%d", opts.DayRpm),
		PoolKVSKeys["max_temp"]:              fmt.Sprintf("%.1f", opts.MaxTemp),

		// Solar-driven hysteresis (#405)
		PoolKVSKeys["solar_enabled"]:           fmt.Sprintf("%t", opts.SolarEnabled),
		PoolKVSKeys["solar_start_threshold_w"]: fmt.Sprintf("%d", opts.SolarStartThresholdW),
		PoolKVSKeys["solar_stop_threshold_w"]:  fmt.Sprintf("%d", opts.SolarStopThresholdW),
		PoolKVSKeys["solar_start_delay_ms"]:    fmt.Sprintf("%d", opts.SolarStartDelayMs),
		PoolKVSKeys["solar_stop_delay_ms"]:     fmt.Sprintf("%d", opts.SolarStopDelayMs),
		PoolKVSKeys["solar_min_turnover"]:      fmt.Sprintf("%d", opts.SolarMinTurnover),
		PoolKVSKeys["solar_max_turnover"]:      fmt.Sprintf("%d", opts.SolarMaxTurnover),
		PoolKVSKeys["solar_stale_ms"]:          fmt.Sprintf("%d", opts.SolarStaleMs),

		// Manual override (#476)
		PoolKVSKeys["override_ms"]: fmt.Sprintf("%d", opts.OverrideMs),
	}

	// Set KVS configuration values with delays to avoid overloading device
	fmt.Printf("  → Configuring %s (%d settings)...\n", sd.Name(), len(kvsConfig))
	for key, value := range kvsConfig {
		if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, key, value); err != nil {
			return fmt.Errorf("failed to set KVS key %s: %w", key, err)
		}
		s.log.V(1).Info("Set KVS value", "key", key, "value", value)
		time.Sleep(500 * time.Millisecond) // Increased delay for device stability
	}

	// Additional pause after KVS operations
	time.Sleep(1 * time.Second)

	// Upload and start script
	scriptName := "pool-pump.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", scriptName, err)
	}

	fmt.Printf("  → Uploading script to %s...\n", sd.Name())
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	minify := !opts.NoMinify
	uploadedID, err := UploadWithVersion(uploadCtx, s.log, via, sd, scriptName, buf, minify, opts.ForceUpload)
	if err != nil {
		return fmt.Errorf("failed to upload/start %s: %w", scriptName, err)
	}

	// UploadWithVersion returns 0 when the version matched and no upload was performed.
	// Fetch the real script ID in that case — schedules reference it by ID, so using 0
	// would create broken schedules that silently fail to call any handler at fire time.
	scriptID := int(uploadedID)
	if scriptID == 0 {
		status, err := pkgscript.ScriptStatus(ctx, sd, via, scriptName)
		if err != nil || status == nil {
			return fmt.Errorf("script version unchanged but failed to get script ID for %s: %w", scriptName, err)
		}
		scriptID = int(status.Id)
		fmt.Printf("  → Script unchanged on %s (id:%d)\n", sd.Name(), scriptID)
	} else {
		fmt.Printf("  → Script uploaded and started on %s (id:%d)\n", sd.Name(), scriptID)
	}

	// Pause to let script initialize and device settle
	time.Sleep(2 * time.Second)

	// Configure device settings
	fmt.Printf("  → Configuring device settings on %s...\n", sd.Name())

	// Disable sys_btn_toggle
	if err := s.disableSysBtnToggle(ctx, via, sd); err != nil {
		s.log.V(1).Info("Failed to disable sys_btn_toggle", "error", err)
	} else {
		fmt.Printf("    Disabled sys_btn_toggle\n")
	}
	time.Sleep(500 * time.Millisecond)

	// Set component names (Pro3 gets all 3, Pro1 gets switch:0 only)
	if err := s.setComponentNames(ctx, via, sd); err != nil {
		s.log.V(1).Info("Failed to set component names", "error", err)
	} else {
		fmt.Printf("    Set switch component names\n")
	}
	time.Sleep(500 * time.Millisecond)

	// Reconcile schedules on every device in the mesh — each device's script
	// self-selects via isMyTurnToRun() so only the preferred device activates.
	if err := s.reconcileSchedules(ctx, via, sd, scriptID); err != nil {
		return fmt.Errorf("failed to reconcile schedules: %w", err)
	}

	s.log.Info("Device setup complete", "device", sd.Name())
	return nil
}

// scheduleDefinition defines a schedule to be created
type scheduleDefinition struct {
	Enable   bool
	Timespec string
	Code     string // The code to execute (e.g., "handleNightStart()")
}

// getDesiredSchedules returns the list of schedules that should exist for the pool pump
func getDesiredSchedules(scriptId int) []scheduleDefinition {
	return []scheduleDefinition{
		{Enable: true, Timespec: "@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT", Code: "handleDailyCheck()"},
		{Enable: false, Timespec: "@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT", Code: "handleMorningStart()"}, // Initially disabled (winter mode)
		{Enable: false, Timespec: "@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT", Code: "handleEveningStop()"},      // Initially disabled (winter mode)
		{Enable: true, Timespec: "0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT", Code: "handleNightStart()"},
		{Enable: true, Timespec: "0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT", Code: "handleNightStop()"},
	}
}

// wrapScheduleJobCode wraps a schedule job's handler call in the standard
// (function(){try{...}catch(e){...}})() form (AGENTS.md kill list rule 1),
// so an uncaught throw inside the handler is contained instead of killing
// the whole script -- a schedule job dispatches through script.eval exactly
// like the ad-hoc probes that rule targets. This restores the protection
// that pool-pump.js's own wrapScheduleCall() used to provide before it
// became dead code (createSchedules() lost its only caller in #504) and was
// removed in #526: schedule creation moved to Go, but the wrapping did not
// move with it until now (#528).
//
// The catch binding is referenced via "if(e&&false){}" rather than left
// empty -- AGENTS.md kill list rule 5: the minifier reduces an empty
// "catch (e) {}" to "catch {}", a syntax error on Espruino.
func wrapScheduleJobCode(code string) string {
	return "(function(){try{" + code + "}catch(e){if(e&&false){}}})()"
}

// buildJobSpec creates a JobSpec from a schedule definition
func buildJobSpec(scriptId int, def scheduleDefinition) schedule.JobSpec {
	return schedule.JobSpec{
		Enable:   def.Enable,
		Timespec: def.Timespec,
		Calls: []schedule.JobCall{{
			Method: "script.eval",
			Params: map[string]interface{}{
				"id":   scriptId,
				"code": wrapScheduleJobCode(def.Code),
			},
		}},
	}
}

// poolPumpScheduleHandlerNames returns the bare handler-call strings (e.g.
// "handleDailyCheck()") that make up every desired schedule job's code --
// the same list getDesiredSchedules() draws from, kept as the single
// authority for "which handler names identify a pool-pump schedule" so
// isPoolPumpScheduleCode below can't drift from it.
func poolPumpScheduleHandlerNames() []string {
	defs := getDesiredSchedules(0) // scriptId is not used to derive Code
	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = def.Code
	}
	return names
}

// isPoolPumpScheduleCode reports whether a Schedule.List job's code field
// belongs to a pool-pump schedule, matching by containment rather than
// equality (mirroring the JS side's onWindowJobs()/updateScheduleMode()/
// verifySchedules() -- see the comment above buildJobSpec). Containment is
// required so this recognizes pool-pump schedules regardless of whether
// their code is bare (devices provisioned before #528) or wrapped (devices
// provisioned after it) -- otherwise a re-provisioning run would fail to
// find and delete a device's already-wrapped jobs before recreating them,
// leaving duplicates behind.
func isPoolPumpScheduleCode(code string) bool {
	if code == "" {
		return false
	}
	for _, name := range poolPumpScheduleHandlerNames() {
		if strings.Contains(code, name) {
			return true
		}
	}
	return false
}

// scheduleState is the on-device Enable/Timespec of an existing schedule
// job, keyed by which handler its code calls.
type scheduleState struct {
	Enable   bool
	Timespec string
}

// preservedScheduleState scans existing for pool-pump schedule jobs
// belonging to scriptId and returns, per handler name (e.g.
// "handleMorningStart()"), the on-device Enable/Timespec that a reconcile
// must carry forward. Matching is by handler-name containment
// (isPoolPumpScheduleCode's mechanism), so it recognizes a job's code
// whether it is bare (pre-#528) or wrapped (post-#528).
func preservedScheduleState(existing []schedule.Job, scriptId int) map[string]scheduleState {
	preserved := make(map[string]scheduleState, len(existing))
	for _, job := range existing {
		if job.Calls == nil || len(job.Calls) == 0 {
			continue
		}
		call := job.Calls[0]
		if call.Method != "script.eval" {
			continue
		}
		params, ok := call.Params.(map[string]interface{})
		if !ok {
			continue
		}
		code, _ := params["code"].(string)
		jobId, _ := params["id"].(float64)
		if int(jobId) != scriptId {
			continue
		}
		for _, name := range poolPumpScheduleHandlerNames() {
			if strings.Contains(code, name) {
				preserved[name] = scheduleState{Enable: job.Enable, Timespec: job.Timespec}
				break
			}
		}
	}
	return preserved
}

// reconcileSchedules deletes all pool-pump schedules and recreates them,
// updating each job's code (e.g. to pick up #528's try/catch wrapping)
// while carrying forward the Enable flag and Timespec of any job that
// already exists for that handler.
//
// getDesiredSchedules() returns fresh-install, winter-mode defaults. Before
// #589, reconcile applied those defaults unconditionally on every run,
// which reset an operational device's schedule state each time `pool add`
// or `pool update` ran: on production this disabled the morning start and
// evening stop (both deliberately enabled for the day's computed window)
// and enabled the night jobs (deliberately disabled in summer mode),
// arming an unauthorized 23:15 pump run. pool-pump.js's own @sunrise daily
// check (handleDailyCheck()) re-asserts the correct enable flags and
// windows, but only once a day -- so the excursion could last up to ~23
// hours. Preserving existing state makes re-provisioning idempotent on an
// already-configured device: only a handler with no existing job (a
// genuinely new schedule) gets the getDesiredSchedules() default.
func (s *PoolService) reconcileSchedules(ctx context.Context, via types.Channel, sd *shelly.Device, scriptId int) error {
	fmt.Printf("  → Managing schedules on %s...\n", sd.Name())

	// Get desired schedules
	desired := getDesiredSchedules(scriptId)

	// List existing schedules
	existing, err := s.listSchedules(ctx, via, sd)
	if err != nil {
		return fmt.Errorf("failed to list schedules: %w", err)
	}

	// Carry forward each handler's on-device Enable/Timespec, if it already
	// has a job, instead of applying getDesiredSchedules()'s defaults.
	preserved := preservedScheduleState(existing, scriptId)
	for i := range desired {
		if state, ok := preserved[desired[i].Code]; ok {
			desired[i].Enable = state.Enable
			desired[i].Timespec = state.Timespec
		}
	}

	// Delete all pool-pump related schedules first
	for _, job := range existing {
		if job.Calls == nil || len(job.Calls) == 0 {
			continue
		}

		call := job.Calls[0]
		if call.Method != "script.eval" {
			continue
		}

		params, ok := call.Params.(map[string]interface{})
		if !ok {
			continue
		}

		code, _ := params["code"].(string)
		jobId, _ := params["id"].(float64)

		if int(jobId) != scriptId {
			continue
		}

		// Check if this is a pool-pump schedule by code pattern (bare or
		// wrapped -- see isPoolPumpScheduleCode).
		if !isPoolPumpScheduleCode(code) {
			continue
		}

		fmt.Printf("    Deleting schedule (id:%d, timespec:%s)...\n", job.Id, job.Timespec)
		if err := s.deleteSchedule(ctx, via, sd, job.Id); err != nil {
			s.log.V(1).Info("Failed to delete schedule", "id", job.Id, "error", err)
			// Continue anyway
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Create all desired schedules
	for _, def := range desired {
		fmt.Printf("    Creating schedule: %s (%s, enable:%v)...\n", def.Code, def.Timespec, def.Enable)
		spec := buildJobSpec(scriptId, def)
		if _, err := s.createSchedule(ctx, via, sd, spec); err != nil {
			return fmt.Errorf("failed to create schedule %s: %w", def.Code, err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("    Schedules recreated: %d created (%d preserved existing enable/timespec)\n", len(desired), len(preserved))
	return nil
}

// listSchedules retrieves all schedules from the device
func (s *PoolService) listSchedules(ctx context.Context, via types.Channel, sd *shelly.Device) ([]schedule.Job, error) {
	params := map[string]interface{}{}
	result, err := sd.CallE(ctx, via, "Schedule.List", params)
	if err != nil {
		return nil, err
	}

	scheduled, ok := result.(*schedule.Scheduled)
	if !ok {
		return nil, fmt.Errorf("unexpected response type from Schedule.List")
	}

	return scheduled.Jobs, nil
}

// createSchedule creates a single schedule on the device
func (s *PoolService) createSchedule(ctx context.Context, via types.Channel, sd *shelly.Device, spec schedule.JobSpec) (*schedule.JobId, error) {
	result, err := sd.CallE(ctx, via, "Schedule.Create", spec)
	if err != nil {
		return nil, err
	}

	jobId, ok := result.(*schedule.JobId)
	if !ok {
		return nil, fmt.Errorf("unexpected response type from Schedule.Create")
	}

	return jobId, nil
}

// deleteSchedule deletes a schedule by ID
func (s *PoolService) deleteSchedule(ctx context.Context, via types.Channel, sd *shelly.Device, id uint32) error {
	params := map[string]interface{}{"id": id}
	_, err := sd.CallE(ctx, via, "Schedule.Delete", params)
	return err
}

// disableSysBtnToggle disables the system button toggle feature
func (s *PoolService) disableSysBtnToggle(ctx context.Context, via types.Channel, sd *shelly.Device) error {
	params := map[string]interface{}{
		"config": map[string]interface{}{
			"device": map[string]interface{}{
				"sys_btn_toggle": false,
			},
		},
	}
	_, err := sd.CallE(ctx, via, "Sys.SetConfig", params)
	return err
}

// setComponentNames configures the switch component names
func (s *PoolService) setComponentNames(ctx context.Context, via types.Channel, sd *shelly.Device) error {
	switchNames := []struct {
		id   int
		name string
	}{
		{0, "pump-eco"},
		{1, "pump-mid"},
		{2, "pump-high"},
	}

	for _, sw := range switchNames {
		params := map[string]interface{}{
			"id": sw.id,
			"config": map[string]interface{}{
				"name": sw.name,
			},
		}
		if _, err := sd.CallE(ctx, via, "Switch.SetConfig", params); err != nil {
			s.log.V(1).Info("Failed to set switch name", "id", sw.id, "name", sw.name, "error", err)
			// Continue anyway, not critical
		}
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

// knownPoolPumpStateKeys are KVS keys written at runtime by pool-pump.js itself
// (via storeValue, which prepends "script/pool-pump/")
var knownPoolPumpStateKeys = []string{
	"script/pool-pump/active-output",
	"script/pool-pump/schedule-mode",
	"script/pool-pump/runtime-sec",    // runtime_today_sec: cumulative pump-on seconds today (#402)
	"script/pool-pump/runtime-ts",     // epoch second STATE.runtimeTodaySec applies to; KVS recovery path (#469)
	"script/pool-pump/turnover-today", // turnover_today: today's achieved water-volume turnovers (#402)
}

// UpdateResult holds the outcome of auditing/updating a single device
type UpdateResult struct {
	DeviceName          string
	DeviceID            string
	DeviceType          string   // "pro3", "pro1", or "unknown"
	MissingKVS          []string // required KVS config keys absent from the device
	StaleKVS            []string // script/pool-pump/* keys on device that are not in the known set
	ScriptUpdated       bool     // true if pool-pump.js was re-uploaded (version changed or forced)
	SchedulesReconciled bool     // true if schedules were reconciled (every device, Pro1 and Pro3 alike -- #589)
	WaterSupplyInvertOK bool     // true if input:0 invert was already correct
	WaterSupplyFixed    bool     // true if input:0 invert was corrected
	Errors              []string
}

// UpdateDevice audits and repairs a pool pump device:
//   - reports KVS config keys from PoolKVSKeys that are absent
//   - reports (returns) stale script/pool-pump/* KVS keys not in the known set
//   - checks and fixes the water-supply input (input:0) invert flag
//   - uploads pool-pump.js if its hash has changed (or force=true)
//   - reconciles schedules (every device -- Pro1 and Pro3 both have and run
//     schedules; there is no Pro3-only restriction, see #589)
func (s *PoolService) UpdateDevice(ctx context.Context, via types.Channel, sd *shelly.Device, force bool, noMinify bool) (*UpdateResult, error) {
	result := &UpdateResult{
		DeviceName: sd.Name(),
		DeviceID:   sd.Id(),
	}

	// Detect device type by switch count (same heuristic as the JS script)
	numSwitches := 0
	for i := 0; i < 3; i++ {
		if _, err := sswitch.GetStatus(ctx, sd, via, i); err != nil {
			break
		}
		numSwitches++
	}
	switch {
	case numSwitches >= 3:
		result.DeviceType = "pro3"
	case numSwitches == 1:
		result.DeviceType = "pro1"
	default:
		result.DeviceType = "unknown"
	}

	// Build the complete set of expected script/pool-pump/* keys
	knownKeys := make(map[string]bool, len(PoolKVSKeys)+len(knownPoolPumpStateKeys))
	for _, v := range PoolKVSKeys {
		knownKeys[v] = true
	}
	for _, k := range knownPoolPumpStateKeys {
		knownKeys[k] = true
	}

	// List all script/pool-pump/* keys currently on the device
	listResp, err := kvs.ListKeys(ctx, s.log, via, sd, "script/pool-pump/*")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to list KVS keys: %v", err))
	} else if listResp != nil {
		foundKeys := make(map[string]bool, len(listResp.Keys))
		for key := range listResp.Keys {
			foundKeys[key] = true
		}

		// Stale: present on device but not in the known set
		for key := range listResp.Keys {
			if !knownKeys[key] {
				result.StaleKVS = append(result.StaleKVS, key)
			}
		}

		// Missing: expected by PoolKVSKeys but absent from device
		for _, v := range PoolKVSKeys {
			if !foundKeys[v] {
				result.MissingKVS = append(result.MissingKVS, v)
			}
		}
	}

	// Verify water-supply input (input:0) has invert=true.
	// The JS script sets this via applyComponentNames() at startup, but we also
	// enforce it here so a mis-configured device is fixed without waiting for
	// the next script restart.
	if cfgRaw, err := sd.CallE(ctx, via, "Input.GetConfig", map[string]interface{}{"id": 0}); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get input:0 config: %v", err))
	} else if cfg, ok := cfgRaw.(*sinput.Configuration); ok {
		if cfg.Invert {
			result.WaterSupplyInvertOK = true
		} else {
			// Fix: set invert=true so water-supply protection works correctly
			fixParams := map[string]interface{}{
				"id": 0,
				"config": map[string]interface{}{
					"invert": true,
				},
			}
			if _, err := sd.CallE(ctx, via, "Input.SetConfig", fixParams); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to fix input:0 invert: %v", err))
			} else {
				result.WaterSupplyFixed = true
			}
		}
	}

	// Update the script (version-tracked; skips upload when hash matches but always restarts)
	scriptName := "pool-pump.js"
	buf, err := pkgscript.ReadEmbeddedFile(scriptName)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read embedded script: %v", err))
		return result, nil
	}

	uploadedID, err := UploadWithVersion(ctx, s.log, via, sd, scriptName, buf, !noMinify, force)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to update script: %v", err))
	} else {
		// UploadWithVersion returns 0 when the version matched and no upload was performed
		result.ScriptUpdated = uploadedID != 0
	}

	// Reconcile schedules on every device -- Pro1 and Pro3 alike. Gating
	// this on DeviceType == "pro3" (as this code did before #589) skipped
	// reconciliation entirely on a Pro1, which is what the production pump
	// (filtration-hiver) is; Pro1 devices plainly have and run schedules.
	// schedules reference the script by ID, so we fetch the current ID after upload.
	scriptStatus, err := pkgscript.ScriptStatus(ctx, sd, via, scriptName)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get script status for schedule reconciliation: %v", err))
	} else if scriptStatus != nil {
		if err := s.reconcileSchedules(ctx, via, sd, int(scriptStatus.Id)); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to reconcile schedules: %v", err))
		} else {
			result.SchedulesReconciled = true
		}
	}

	return result, nil
}

// Speed represents pump speed
type Speed string

const (
	SpeedEco  Speed = "eco"
	SpeedMid  Speed = "mid"
	SpeedHigh Speed = "high"
)

// speedMappings holds the switch ID for each speed
type speedMappings struct {
	Eco  int
	Mid  int
	High int
}

// defaultSpeedMappings are the eco/mid/high switch indices used when a
// controller has no speed mapping configured in KVS yet.
var defaultSpeedMappings = speedMappings{Eco: 0, Mid: 1, High: 2}

// parseSpeedMappings turns up to three raw KVS string values into a
// speedMappings, falling back to defaultSpeedMappings for any value that's
// missing (nil) or fails to parse as an integer.
func parseSpeedMappings(ecoRaw, midRaw, highRaw *string) *speedMappings {
	mappings := defaultSpeedMappings
	if ecoRaw != nil {
		fmt.Sscanf(*ecoRaw, "%d", &mappings.Eco)
	}
	if midRaw != nil {
		fmt.Sscanf(*midRaw, "%d", &mappings.Mid)
	}
	if highRaw != nil {
		fmt.Sscanf(*highRaw, "%d", &mappings.High)
	}
	return &mappings
}

// getSpeedMappings retrieves speed-to-switch mappings from controller's KVS
func (s *PoolService) getSpeedMappings(ctx context.Context, sd *shelly.Device, via types.Channel) (*speedMappings, error) {
	var eco, mid, high *string
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["eco_speed"]); err == nil && val != nil {
		eco = &val.Value
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["mid_speed"]); err == nil && val != nil {
		mid = &val.Value
	}
	if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys["high_speed"]); err == nil && val != nil {
		high = &val.Value
	}
	return parseSpeedMappings(eco, mid, high), nil
}

// Start starts the pool pump at the specified speed
func (s *PoolService) Start(ctx context.Context, deviceID string, speed Speed) error {
	s.log.Info("Starting pool pump", "device", deviceID, "speed", speed)

	// Get device
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault

	// Get speed mappings from KVS
	mappings, err := s.getSpeedMappings(ctx, sd, via)
	if err != nil {
		return fmt.Errorf("failed to get speed mappings: %w", err)
	}

	// Map speed to switch ID
	var switchID int
	switch speed {
	case SpeedEco:
		switchID = mappings.Eco
	case SpeedMid:
		switchID = mappings.Mid
	case SpeedHigh:
		switchID = mappings.High
	default:
		return fmt.Errorf("invalid speed: %s", speed)
	}

	// Call doStart() in the script to use unified logic.
	// doStart() expects a speed string ('eco'/'mid'/'high'), not a switch ID integer.
	code := fmt.Sprintf("doStart('%s', 'Manual start via ctl pool start %s')", speed, speed)

	result, err := pkgscript.EvalInDevice(ctx, via, sd, "pool-pump.js", code)
	if err != nil {
		return fmt.Errorf("failed to start pump via script: %w", err)
	}

	s.log.Info("Pump start command sent", "speed", speed, "switch", switchID, "result", result)
	return nil
}

// Stop stops the pool pump on the specified device
func (s *PoolService) Stop(ctx context.Context, deviceID string) error {
	s.log.Info("Stopping pool pump", "device", deviceID)

	// Get device
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault

	// Call doStop() in the script to use unified logic
	code := "doStop('Manual stop via ctl pool stop')"

	result, err := pkgscript.EvalInDevice(ctx, via, sd, "pool-pump.js", code)
	if err != nil {
		return fmt.Errorf("failed to stop pump via script: %w", err)
	}

	s.log.Info("Pump stop command sent", "result", result)
	return nil
}

// AddDevice adds a single device to the pool pump mesh
// AddDevice configures one pump controller. There is no mesh and no peer list:
// exactly one controller is wired to the pump at a time and never coordinates
// with another.
func (s *PoolService) AddDevice(ctx context.Context, via types.Channel, sd *shelly.Device, deviceID string, opts SetupOptions) error {
	s.log.Info("Configuring pool pump controller", "device", sd.Name(), "id", deviceID)

	return s.setupDevice(ctx, via, sd, deviceID, opts)
}

// SetPreferred sets the preferred device ID and speed on a device
// SetSpeed sets the speed a controller uses when it starts the pump.
//
// There is no longer a preferred *device* to set: exactly one controller is
// wired at a time, so a device runs on its own schedule with no election.
func (s *PoolService) SetSpeed(ctx context.Context, via types.Channel, sd *shelly.Device, speed string) error {
	s.log.Info("Setting pump speed", "device", sd.Name(), "speed", speed)

	if _, err := kvs.SetKeyValue(ctx, s.log, via, sd, PoolKVSKeys["preferred_speed"], speed); err != nil {
		return fmt.Errorf("failed to set preferred_speed: %w", err)
	}

	s.log.Info("Pump speed set", "device", sd.Name(), "speed", speed)
	return nil
}

// RemoveDevice removes a device from the pool pump mesh
func (s *PoolService) RemoveDevice(ctx context.Context, via types.Channel, sd *shelly.Device) error {
	s.log.Info("Removing device from pool pump mesh", "device", sd.Name())

	// Stop and delete the script
	scriptName := "pool-pump.js"
	if _, err := pkgscript.StartStopDelete(ctx, via, sd, scriptName, pkgscript.Delete); err != nil {
		s.log.V(1).Info("Failed to stop/delete script", "error", err)
		// Continue to clear KVS even if script deletion fails
	}

	// Clear all pool-pump KVS keys
	kvsKeys := []string{
		PoolKVSKeys["enable_logging"],
		PoolKVSKeys["mqtt_topic_prefix"],
		PoolKVSKeys["preferred_device_id"],
		PoolKVSKeys["preferred_speed"],
		PoolKVSKeys["pro3_device_id"],
		PoolKVSKeys["pro1_device_id"],
		PoolKVSKeys["eco_speed"],
		PoolKVSKeys["mid_speed"],
		PoolKVSKeys["high_speed"],
		PoolKVSKeys["night_run_duration_ms"],
		PoolKVSKeys["grace_delay_ms"],
		PoolKVSKeys["temperature_threshold"],
		PoolKVSKeys["pool_volume"],
		PoolKVSKeys["turnover"],
		PoolKVSKeys["max_flow_rate"],
		PoolKVSKeys["max_rpm"],
		PoolKVSKeys["eco_rpm"],
		PoolKVSKeys["mid_rpm"],
		PoolKVSKeys["high_rpm"],
		PoolKVSKeys["max_temp"],
		PoolKVSKeys["solar_enabled"],
		PoolKVSKeys["solar_start_threshold_w"],
		PoolKVSKeys["solar_stop_threshold_w"],
		PoolKVSKeys["solar_start_delay_ms"],
		PoolKVSKeys["solar_stop_delay_ms"],
		PoolKVSKeys["solar_min_turnover"],
		PoolKVSKeys["solar_max_turnover"],
		PoolKVSKeys["solar_stale_ms"],
		PoolKVSKeys["override_ms"],
	}

	for _, key := range kvsKeys {
		if _, err := kvs.DeleteKey(ctx, s.log, via, sd, key); err != nil {
			s.log.V(1).Info("Failed to delete KVS key", "key", key, "error", err)
			// Continue deleting other keys
		}
	}

	s.log.Info("Device removed from pool pump mesh", "device", sd.Name())
	return nil
}

// Purge removes pool pump setup from a single device
func (s *PoolService) Purge(ctx context.Context, deviceID string) error {
	s.log.Info("Purging pool pump setup", "device", deviceID)

	// Get device
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelMqtt

	// Purge device
	fmt.Printf("  → Purging device %s...\n", sd.Name())
	if err := s.purgeDevice(ctx, via, sd); err != nil {
		return fmt.Errorf("failed to purge device: %w", err)
	}

	s.log.Info("Pool pump setup purged", "device", deviceID)
	return nil
}

func (s *PoolService) purgeDevice(ctx context.Context, via types.Channel, sd *shelly.Device) error {
	scriptName := "pool-pump.js"

	// Step 1: Stop all switches (best effort)
	s.log.V(1).Info("Stopping all switches", "device", sd.Name())
	for i := 0; i < 3; i++ {
		params := map[string]interface{}{"id": i, "on": false}
		if _, err := sd.CallE(ctx, via, "Switch.Set", params); err != nil {
			s.log.V(1).Info("Failed to stop switch (ignoring)", "switch", i, "error", err)
		}
	}

	// Step 2: List all KVS keys
	s.log.V(1).Info("Listing KVS keys", "device", sd.Name())
	listResp, err := kvs.ListKeys(ctx, s.log, via, sd, "*")
	if err != nil {
		s.log.Error(err, "Failed to list KVS keys", "device", sd.Name())
	} else if listResp != nil {
		// Step 3: Delete all pool-pump related KVS keys
		var keysToDelete []string
		for key := range listResp.Keys {
			if len(key) >= 16 && key[:16] == "script/pool-pump" {
				keysToDelete = append(keysToDelete, key)
			}
			// Also clean old pool/ prefix keys
			if len(key) >= 5 && key[:5] == "pool/" {
				keysToDelete = append(keysToDelete, key)
			}
			// Also clean script/pool/ prefix keys
			if len(key) >= 12 && key[:12] == "script/pool/" {
				keysToDelete = append(keysToDelete, key)
			}
		}

		s.log.V(1).Info("Deleting KVS keys", "device", sd.Name(), "count", len(keysToDelete))
		for _, key := range keysToDelete {
			if _, err := kvs.DeleteKey(ctx, s.log, via, sd, key); err != nil {
				s.log.V(1).Info("Failed to delete KVS key (ignoring)", "key", key, "error", err)
			}
		}
	}

	// Step 4: Stop script (if running)
	s.log.V(1).Info("Stopping script", "device", sd.Name(), "script", scriptName)
	if _, err := pkgscript.StartStopDelete(ctx, via, sd, scriptName, pkgscript.Stop); err != nil {
		s.log.V(1).Info("Failed to stop script (may not be running)", "error", err)
	}

	// Step 5: Delete script
	s.log.V(1).Info("Deleting script", "device", sd.Name(), "script", scriptName)
	if _, err := pkgscript.StartStopDelete(ctx, via, sd, scriptName, pkgscript.Delete); err != nil {
		s.log.V(1).Info("Failed to delete script (may not exist)", "error", err)
	}

	s.log.Info("Device cleaned", "device", sd.Name())
	return nil
}

// PoolStatus represents the status of the pool pump system
type PoolStatus struct {
	Devices     []DeviceStatus `json:"devices" yaml:"devices"`
	Environment Environment    `json:"environment" yaml:"environment"`
}

// DeviceStatus represents the status of a single device
type DeviceStatus struct {
	DeviceID         string          `json:"device_id" yaml:"device_id"`
	DeviceName       string          `json:"device_name,omitempty" yaml:"device_name,omitempty"`
	DeviceType       string          `json:"device_type,omitempty" yaml:"device_type,omitempty"` // "pro3" or "pro1"
	Role             string          `json:"role" yaml:"role"`
	Online           bool            `json:"online" yaml:"online"`
	ScriptRunning    bool            `json:"script_running" yaml:"script_running"`
	ActiveOutput     int             `json:"active_output" yaml:"active_output"` // -1 = off, 0/1/2 = switch ID
	ActiveSpeed      string          `json:"active_speed,omitempty" yaml:"active_speed,omitempty"`
	SavedOutput      int             `json:"saved_output,omitempty" yaml:"saved_output,omitempty"` // Saved before water-supply protection
	Inputs           map[string]bool `json:"inputs" yaml:"inputs"`
	Switches         map[string]int  `json:"switches,omitempty" yaml:"switches,omitempty"` // Switch names to IDs
	MqttConnected    bool            `json:"mqtt_connected,omitempty" yaml:"mqtt_connected,omitempty"`
	LastRunTimestamp *int64          `json:"last_run_timestamp,omitempty" yaml:"last_run_timestamp,omitempty"` // Unix timestamp
	ScheduleMode     string          `json:"schedule_mode,omitempty" yaml:"schedule_mode,omitempty"`           // "summer" or "winter"
	Error            string          `json:"error,omitempty" yaml:"error,omitempty"`
}

// Environment represents environmental conditions and configuration
type Environment struct {
	// Temperature threshold for winter mode
	TemperatureThreshold float64 `json:"temperature_threshold" yaml:"temperature_threshold"`

	// Schedule configuration
	ScheduleMode string `json:"schedule_mode" yaml:"schedule_mode"` // "winter", "manual", etc.

	// Speed mappings (controller only)
	EcoSpeed  *int `json:"eco_speed,omitempty" yaml:"eco_speed,omitempty"`
	MidSpeed  *int `json:"mid_speed,omitempty" yaml:"mid_speed,omitempty"`
	HighSpeed *int `json:"high_speed,omitempty" yaml:"high_speed,omitempty"`

	// MQTT configuration
	MqttTopicPrefix string `json:"mqtt_topic_prefix,omitempty" yaml:"mqtt_topic_prefix,omitempty"`

	// Weather forecast
	ForecastUrl string `json:"forecast_url,omitempty" yaml:"forecast_url,omitempty"`

	// Temperature-proportional scheduling parameters
	PoolVolume  int     `json:"pool_volume,omitempty"   yaml:"pool_volume,omitempty"`
	Turnover    int     `json:"turnover,omitempty"      yaml:"turnover,omitempty"`
	MaxFlowRate int     `json:"max_flow_rate,omitempty" yaml:"max_flow_rate,omitempty"`
	MaxRpm      int     `json:"max_rpm,omitempty"       yaml:"max_rpm,omitempty"`
	EcoRpm      int     `json:"eco_rpm,omitempty"       yaml:"eco_rpm,omitempty"`
	MidRpm      int     `json:"mid_rpm,omitempty"       yaml:"mid_rpm,omitempty"`
	HighRpm     int     `json:"high_rpm,omitempty"      yaml:"high_rpm,omitempty"`
	MaxTemp     float64 `json:"max_temp,omitempty"      yaml:"max_temp,omitempty"`
}

// Status returns the status of a single device
func (s *PoolService) Status(ctx context.Context, deviceID string) (*DeviceStatus, error) {
	s.log.Info("Getting pool pump status", "device", deviceID)

	// Resolve device identifier
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	// Get device status
	status, err := s.getDeviceStatus(ctx, device.Id())
	if err != nil {
		return nil, fmt.Errorf("failed to get device status: %w", err)
	}

	return &status, nil
}

func (s *PoolService) getDeviceStatus(ctx context.Context, deviceID string) (DeviceStatus, error) {
	// Get device to retrieve name
	device, err := s.provider.GetDeviceByAny(ctx, deviceID)
	if err != nil {
		return DeviceStatus{}, err
	}

	status := DeviceStatus{
		DeviceID:   deviceID,
		DeviceName: device.Name(),
		Inputs:     make(map[string]bool),
		Switches:   make(map[string]int),
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return status, fmt.Errorf("failed to get shelly device: %w", err)
	}

	via := types.ChannelDefault
	status.Online = true

	// Get script status
	scriptStatus, err := pkgscript.ScriptStatus(ctx, sd, via, "pool-pump.js")
	if err == nil && scriptStatus != nil {
		status.ScriptRunning = scriptStatus.Running
	}

	// Get switch status to determine active output and device type
	status.ActiveOutput = -1
	status.ActiveSpeed = "off"

	// Detect device type by trying to read switches
	numSwitches := 0
	for i := 0; i < 3; i++ {
		result, err := sswitch.GetStatus(ctx, sd, via, i)
		if err != nil {
			break // Stop at first error (Pro1 has only 1 switch)
		}
		numSwitches++
		if result != nil && result.Output {
			status.ActiveOutput = i
			break
		}
	}

	// Get speed mappings for speed name display
	var mappings *speedMappings
	if numSwitches >= 3 {
		if m, err := s.getSpeedMappings(ctx, sd, via); err == nil {
			mappings = m
		}
		// Map switch ID back to speed name
		if status.ActiveOutput >= 0 && mappings != nil {
			if status.ActiveOutput == mappings.Eco {
				status.ActiveSpeed = "eco"
			} else if status.ActiveOutput == mappings.Mid {
				status.ActiveSpeed = "mid"
			} else if status.ActiveOutput == mappings.High {
				status.ActiveSpeed = "high"
			} else {
				status.ActiveSpeed = fmt.Sprintf("switch-%d", status.ActiveOutput)
			}
		}
	}

	// Get input status (all devices have water-supply, high-water, and max-speed-active inputs)
	inputNames := []string{"water-supply", "high-water", "max-speed-active"}
	for i, name := range inputNames {
		params := map[string]interface{}{"id": i}
		result, err := sd.CallE(ctx, via, "Input.GetStatus", params)
		if err == nil && result != nil {
			if statusMap, ok := result.(map[string]interface{}); ok {
				if state, ok := statusMap["state"].(bool); ok {
					status.Inputs[name] = state
				}
			}
		}
	}

	// Get switch names from device config based on device type
	var switchNames []string
	if numSwitches >= 3 {
		switchNames = []string{"pump-eco", "pump-mid", "pump-high"}
	} else {
		switchNames = []string{"pump-max"}
	}
	for i, name := range switchNames {
		if i < numSwitches {
			status.Switches[name] = i
		}
	}

	// Determine device type from number of switches
	if numSwitches >= 3 {
		status.DeviceType = "pro3"
	} else {
		status.DeviceType = "pro1"
	}

	// Get MQTT connection status
	params := map[string]interface{}{}
	if result, err := sd.CallE(ctx, via, "MQTT.GetStatus", params); err == nil && result != nil {
		if mqttStatus, ok := result.(map[string]interface{}); ok {
			if connected, ok := mqttStatus["connected"].(bool); ok {
				status.MqttConnected = connected
			}
		}
	}

	// Get state from KVS (all devices can have these)
	// Get last run timestamp
	if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/last-run-ts"); err == nil && val != nil && val.Value != "" {
		var ts int64
		if _, err := fmt.Sscanf(val.Value, "%d", &ts); err == nil {
			status.LastRunTimestamp = &ts
		}
	}

	// Get schedule mode
	if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/schedule-mode"); err == nil && val != nil && val.Value != "" {
		status.ScheduleMode = val.Value
	}

	// Get saved output (for water-supply protection)
	if val, err := kvs.GetValue(ctx, s.log, via, sd, "script/pool-pump/active-output"); err == nil && val != nil && val.Value != "" {
		var savedOut int
		if _, err := fmt.Sscanf(val.Value, "%d", &savedOut); err == nil {
			status.SavedOutput = savedOut
		}
	}

	return status, nil
}

// environmentKVSFields lists the PoolKVSKeys names read by getEnvironmentStatus,
// in fetch order.
var environmentKVSFields = []string{
	"temperature_threshold", "eco_speed", "mid_speed", "high_speed",
	"pool_volume", "turnover", "max_flow_rate", "max_rpm", "eco_rpm", "mid_rpm", "high_rpm",
	"max_temp", "mqtt_topic_prefix",
}

// applyEnvironmentKVSValues parses the raw KVS string values successfully
// fetched for controllerID (keyed by the same names as environmentKVSFields)
// and assigns them onto env. Missing keys and unparseable values are left
// untouched, matching the original fetch-and-best-effort-parse behavior.
func applyEnvironmentKVSValues(env *Environment, raw map[string]string) {
	if v, ok := raw["temperature_threshold"]; ok {
		var threshold float64
		if _, err := fmt.Sscanf(v, "%f", &threshold); err == nil {
			env.TemperatureThreshold = threshold
		}
	}
	if v, ok := raw["eco_speed"]; ok {
		var speed int
		if _, err := fmt.Sscanf(v, "%d", &speed); err == nil {
			env.EcoSpeed = &speed
		}
	}
	if v, ok := raw["mid_speed"]; ok {
		var speed int
		if _, err := fmt.Sscanf(v, "%d", &speed); err == nil {
			env.MidSpeed = &speed
		}
	}
	if v, ok := raw["high_speed"]; ok {
		var speed int
		if _, err := fmt.Sscanf(v, "%d", &speed); err == nil {
			env.HighSpeed = &speed
		}
	}
	if v, ok := raw["pool_volume"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.PoolVolume = i
		}
	}
	if v, ok := raw["turnover"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.Turnover = i
		}
	}
	if v, ok := raw["max_flow_rate"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.MaxFlowRate = i
		}
	}
	if v, ok := raw["max_rpm"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.MaxRpm = i
		}
	}
	if v, ok := raw["eco_rpm"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.EcoRpm = i
		}
	}
	if v, ok := raw["mid_rpm"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.MidRpm = i
		}
	}
	if v, ok := raw["high_rpm"]; ok {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			env.HighRpm = i
		}
	}
	if v, ok := raw["max_temp"]; ok {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			env.MaxTemp = f
		}
	}
	if v, ok := raw["mqtt_topic_prefix"]; ok {
		env.MqttTopicPrefix = v
	}
}

func (s *PoolService) getEnvironmentStatus(ctx context.Context, controllerID string, env *Environment) error {
	device, err := s.provider.GetDeviceByAny(ctx, controllerID)
	if err != nil {
		return err
	}

	sd, err := s.provider.GetShellyDevice(ctx, device)
	if err != nil {
		return err
	}

	via := types.ChannelMqtt

	raw := make(map[string]string)
	for _, key := range environmentKVSFields {
		if val, err := kvs.GetValue(ctx, s.log, via, sd, PoolKVSKeys[key]); err == nil && val != nil {
			raw[key] = val.Value
		}
	}
	applyEnvironmentKVSValues(env, raw)

	// Get forecast URL from Script.storage (not KVS)
	// Note: This is stored in Script.storage, not KVS, so we'd need to call Script.Eval to retrieve it
	// For now, we'll skip this as it requires executing code on the device

	// Note: The script tracks last run time and makes bootstrap decisions internally
	// We only display the configuration here, not the runtime state

	return nil
}
