# Release Notes Template

> **Note**: This is a template for creating release notes. Copy this file and replace placeholders with actual release information.

## Release Information

- **Version**: `vX.Y.Z`
- **Release Date**: `YYYY-MM-DD`
- **Previous Version**: `vX.Y.Z`

---

# Release Notes - Template

## 🎯 Critical Fixes

<!-- List critical bug fixes that address security issues, data loss, crashes, or major functionality problems -->

### Example Section
- **Issue description** - Brief explanation of the fix and its impact
- **Another critical fix** - Description

## 🚀 New Features

<!-- List new features and capabilities added in this release -->

### Category Name
- **Feature name** - Description of what it does and why it's useful
- **Another feature** - Description

## 🔧 Improvements & Refactoring

<!-- List improvements to existing features, performance enhancements, and code refactoring -->

### Code Organization
- **Improvement description** - What was improved and why

### Performance
- **Optimization description** - What was optimized

## 🐛 Bug Fixes

<!-- List non-critical bug fixes -->

- **Bug description** - What was fixed
- **Another bug** - What was fixed

## 📚 Documentation

<!-- List documentation updates, new guides, or improved examples -->

- **Documentation item** - What was added or updated
- **Another doc update** - Description

## 🔄 Migration Notes

<!-- Instructions for users upgrading from previous versions -->

### For Existing Deployments

1. **Step-by-step migration instructions**
   ```bash
   # Example commands
   sudo systemctl daemon-reload
   sudo systemctl restart myhome
   ```

2. **Configuration changes** (if any)
   - List any configuration file changes
   - Provide examples

### Breaking Changes

<!-- List any breaking changes that require user action -->

- **Breaking change description** - What changed and what users need to do
- Or: `None - This release is fully backward compatible with vX.Y.Z`

## 📦 Installation

### Debian/Ubuntu (ARM64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/vX.Y.Z/myhome_X.Y.Z_arm64.deb
sudo dpkg -i myhome_X.Y.Z_arm64.deb
```

### Debian/Ubuntu (AMD64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/vX.Y.Z/myhome_X.Y.Z_amd64.deb
sudo dpkg -i myhome_X.Y.Z_amd64.deb
```

### Windows
Download and run `MyHome-X.Y.Z.msi` from the [releases page](https://github.com/asnowfix/home-automation/releases/tag/vX.Y.Z).

## 🙏 Contributors

<!-- Acknowledge contributors, testers, and community members -->

This release includes contributions from the community and extensive testing on production systems.

Special thanks to:
- Contributor names (if applicable)

---

## 📋 Checklist for Creating Release Notes

When creating release notes for a new version:

- [ ] Update version numbers (vX.Y.Z) throughout the document
- [ ] Update release date
- [ ] Update previous version reference
- [ ] Fill in all sections with actual changes from git log
- [ ] Update installation URLs with correct version
- [ ] Review breaking changes section carefully
- [ ] Add migration instructions if needed
- [ ] Test installation commands
- [ ] Proofread for clarity and accuracy

## 🔗 Useful Commands

```bash
# Get commits between versions
git log vX.Y.Z..vX.Y.Z --oneline

# Get detailed commit messages
git log vX.Y.Z..vX.Y.Z --pretty=format:"%h %s" --reverse

# Generate GitHub comparison URL
echo "https://github.com/asnowfix/home-automation/compare/vX.Y.Z...vX.Y.Z"
```

---

**Full Changelog**: https://github.com/asnowfix/home-automation/compare/vPREVIOUS...vCURRENT

---

# Release Notes - v0.5.1

**Release Date**: 2025-10-05  
**Previous Version**: v0.5.0

---

## 🎯 Critical Fixes

### Daemon Stability & Reliability
- **Fixed daemon exit after 15 seconds** - Removed command timeout for daemon processes to allow indefinite execution
- **Fixed systemd service not restarting on failure** - Changed `Restart=on-failure` to `Restart=always` with rate limiting (5 restarts in 5 minutes)
- **Added MQTT connection watchdog** - Monitors MQTT connection health and triggers daemon restart after configurable consecutive failures (default: 3 failures × 30s = 90s)
- **Configurable watchdog parameters** - New CLI flags: `--mqtt-watchdog-interval` and `--mqtt-watchdog-max-failures`

### MQTT Client Improvements
- **Lazy watchdog initialization** - Watchdog now starts automatically on first MQTT connection
- **Automatic reconnection** - Paho MQTT client configured with `SetAutoReconnect(true)` and `SetResumeSubs(true)` for automatic recovery
- **Connection monitoring** - Watchdog monitors connection without interfering with Paho's built-in auto-reconnect mechanism

### CI/CD Fixes
- **Fixed duplicate tag creation** - Package release workflow now uses existing `v*.*.*` tags instead of creating duplicate tags without `v` prefix
- **Improved tag propagation** - Added 5-second delay after tag creation to ensure propagation before triggering dependent workflows
- **Fixed version tag format** - Updated GitHub Actions workflows to use v_minor and v_patch outputs correctly

## 🚀 New Features

### Shelly Device Management
- **IPv6 support** - Added IPv6 address resolution for Shelly devices
- **Improved device setup** - Reorganized setup process with detailed progress output and better error handling
- **Script version tracking** - Added version comparison to detect out-of-date scripts on devices
- **VSCode launch configurations** - Added debug configurations for Shelly script management
- **Improved IP resolution** - Try harder to get IP addresses from ZeroConf/mDNS announcements
- **Parking light control** - Added parking light control and updated external stairs switch mapping

### BLE & Event Handling
- **Pool motion sensor support** - Added event data structure for Shelly BLE motion sensors
- **Remote input event handling** - Improved event structure validation and remote input support
- **Auto-off timer cancellation** - BLU-triggered timers are cancelled when manual switch operation is detected
- **Illuminance tracking** - Only include illuminance values when flag changes and value is positive
- **Percentage-based thresholds** - Support for illuminance thresholds based on 7-day history
- **Precipitation-based irrigation control** - Added irrigation control script for Shelly devices (WIP)

### Logging System
- **Configurable log levels** - Added `--log-level` flag (error, warn, info, debug)
- **Per-package loggers** - Better log organization with package-aware logging
- **Debugger detection** - Automatic timeout adjustment when running under debugger
- **Comprehensive documentation** - Added logging system guide with examples

### UI Improvements
- **Bulma CSS framework** - Switched to modern Bulma framework for better UI
- **Shelly Cloud integration** - Added link to Shelly Control Cloud

## 🔧 Improvements & Refactoring

### Code Organization
- **Renamed homectl to myhome/ctl** - Improved package naming consistency
- **Consolidated Shelly subcommands** - Better command organization
- **Extracted reusable functions** - Reduced callback nesting in Shelly scripts (JavaScript engine compatibility)
- **Parallelized device operations** - Improved performance for bulk device updates
- **Reorganized imports** - Better import organization in Shelly setup package

### Script Management
- **Improved upload/update logging** - Better progress feedback with `--force` flag option
- **KVS key processing** - Extracted reusable functions and skip non-follow keys during loading
- **Script lifecycle logging** - Added startup and stop event logging for better debugging
- **Device reboot logging** - Moved log message to appear right after successful reboot

### Build & Packaging
- **AMD64 architecture support** - Added support for x86_64 builds
- **Dynamic signtool detection** - Improved MSI signing workflow for Windows packages
- **Windows SDK installation** - Automated SDK setup in CI/CD pipeline
- **Version extraction improvements** - Better handling of version tags in workflows
- **Custom patch version calculation** - Replaced semver action with custom calculation

## 🐛 Bug Fixes

- **ShellyProX button events** - Fixed handling of (sys) button events
- **Event handler logic** - Corrected logic for remote and local input events
- **Invalid device info** - Replaced panic with error return for better error handling
- **KVS loading** - Fixed script crashes due to excessive callback nesting
- **Context cancellation** - Improved context cancellation logging

## 📚 Documentation

- **AI Coding Agent guidelines** - Added comprehensive instructions for AI assistants (AGENTS.md)
- **Shelly JS engine documentation** - Updated with ES5 feature support and best practices
- **Logging system guide** - Complete documentation with configuration examples
- **Release process documentation** - Added comprehensive release workflow guide with Makefile integration

## 🔄 Migration Notes

### For Existing Deployments

1. **Update systemd service files** - The new service configuration includes:
   - `Restart=always` instead of `Restart=on-failure`
   - Rate limiting: `StartLimitIntervalSec=300` and `StartLimitBurst=5`

2. **Reload systemd configuration**:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart myhome
   ```

3. **Optional: Configure watchdog parameters**:
   ```bash
   # Match Shelly device timing (50s total)
   ExecStart=/usr/bin/myhome daemon run --mqtt-watchdog-interval 10s --mqtt-watchdog-max-failures 5
   
   # More lenient (5 minutes total)
   ExecStart=/usr/bin/myhome daemon run --mqtt-watchdog-interval 60s --mqtt-watchdog-max-failures 5
   ```

### Breaking Changes

None - This release is fully backward compatible with v0.5.0

## 📦 Installation

### Debian/Ubuntu (ARM64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/v0.5.1/myhome_0.5.1_arm64.deb
sudo dpkg -i myhome_0.5.1_arm64.deb
```

### Debian/Ubuntu (AMD64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/v0.5.1/myhome_0.5.1_amd64.deb
sudo dpkg -i myhome_0.5.1_amd64.deb
```

### Windows
Download and run `MyHome-0.5.1.msi` from the [releases page](https://github.com/asnowfix/home-automation/releases/tag/v0.5.1).

---

**Full Changelog**: https://github.com/asnowfix/home-automation/compare/v0.5.0...v0.5.1

---

# Release Notes - v0.4.4

**Release Date**: 2024-09-23  
**Previous Version**: v0.3.8

---

## 🚀 New Features

### Prometheus Monitoring
- **Prometheus metrics endpoint** - Added comprehensive metrics collection for Shelly device monitoring
- **Scrape config generator** - Automatic Prometheus configuration generation
- **Memory-optimized metrics** - Reduced string allocations in metric generation

### BLE Device Support
- **BLU sensor integration** - Added Shelly BLU sensor event data structure with BTHome fields
- **Split BLE architecture** - Separated BLE scanner into publisher and listener scripts for better separation of concerns
- **MAC address normalization** - Standardized MAC address handling
- **BLU device following** - Configuration-based device following with KVS storage
- **MQTT publishing** - BLE events published to MQTT topics

### Device Management
- **Reverse proxy** - Added reverse proxy to connect to known devices with WebSocket support
- **Device UI in separate window** - Open device interfaces in dedicated windows
- **Device refresh action** - SSE completion notifications for refresh operations
- **DNS-SD service publishing** - Lazy mDNS resolver initialization
- **Device lookup by identifier** - Find devices using various identifiers

### Script Management
- **Script minification** - Added `--no-minify` flag for Shelly script uploads
- **ES6-safe minification** - Minification without ES6 string templates (Shelly compatibility)
- **Script-level debug control** - Per-script debugging via Script.Eval
- **Version tracking** - Store software version for each script
- **Watchdog script priority** - Ensure watchdog.js is the first script

### Device Control
- **Switch command** - Added on/off/toggle subcommands for device control
- **RPC call command** - Direct Shelly device RPC interaction
- **Input type support** - Toggle action on button press in status listener
- **Single push button events** - Support for button-configured inputs

### Status Monitoring
- **Status listener** - Follow Shelly device status changes via MQTT
- **Event-based KVS updates** - Replaced periodic refresh with KVS change events
- **Device following** - Support for both Shelly and BLU devices

## 🔧 Improvements & Refactoring

### Performance
- **Atomic group operations** - Optimized group upsert with atomic operations (fixed race condition)
- **Reentrant mutex** - Thread-safe device operations with context tokens
- **500ms command delay** - Prevent command conflicts on same device

### Code Organization
- **Renamed listenblu to "listen blu"** - Better package clarity
- **Merged KVS event handling** - Consolidated event subscription handlers
- **Migrated Gen1 proxy** - Moved to pkg/shelly/gen1
- **Removed deprecated packages** - Cleaned up old listenblu implementation

### Configuration
- **WiFi/MQTT/System APIs** - Updated to use channel parameter
- **Force public NTP pools** - Improved time synchronization
- **Default proxy port** - Changed from 8080 to 6080

### Logging & Debugging
- **RFC3339 timestamps** - Standardized time format
- **Debug on/off toggle** - Runtime debug control
- **MQTT event logging** - Log all MQTT events for testing
- **Reduced verbosity** - Less noisy debug logging in status-listener
- **Better context** - Improved logging context in MQTT broker

## 🐛 Bug Fixes

- **Device initialization** - Use init() instead of refresh() for new devices
- **Group upsert race condition** - Fixed with atomic operations
- **Matter property support** - Handle new "matter" field in DeviceInfo
- **Null device names** - Support null names from Shelly Plug G3
- **Unconfigured UDP RPC** - Work around empty string for host:port
- **HTTP transient errors** - Automatic retry with exponential backoff
- **Setup timeouts** - Use long-lived context to prevent timeouts
- **Illuminance bounds** - Use strict comparisons and correct default min/max
- **IP resolution** - Improved logic and logging in mDNS discovery
- **Hostname handling** - Better zeroconf device discovery
- **WiFi settings** - Fixed wifi-setting to avoid disabling everything
- **Device host updates** - Update host during refresh
- **MQTT topic formatting** - Correct formatting for Gen2 events
- **Negative job ID** - Removed redundant error check in cancel command
- **WebSocket proxy** - Removed debug logging from patch

## 📚 Documentation

- **Device following guide** - Comprehensive documentation for Shelly and BLU device following
- **Data collector** - Added Shelly device data collector for API testing and documentation
- **JSDoc type definitions** - BLE MQTT listener configuration types

## 🔄 CI/CD Improvements

- **Auto-tagging constraints** - Constrain to specific version branches using regex matching

## 📦 Installation

### Debian/Ubuntu (ARM64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/v0.4.4/myhome_0.4.4_arm64.deb
sudo dpkg -i myhome_0.4.4_arm64.deb
```

### Debian/Ubuntu (AMD64)
```bash
wget https://github.com/asnowfix/home-automation/releases/download/v0.4.4/myhome_0.4.4_amd64.deb
sudo dpkg -i myhome_0.4.4_amd64.deb
```

### Windows
Download and run `MyHome-0.4.4.msi` from the [releases page](https://github.com/asnowfix/home-automation/releases/tag/v0.4.4).

---

**Full Changelog**: https://github.com/asnowfix/home-automation/compare/v0.3.8...v0.4.4