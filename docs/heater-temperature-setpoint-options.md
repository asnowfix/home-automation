# Heater Temperature Set-Point Architecture Options

## Current State

The `heater.js` script currently uses:
- **Single static set-point**: Configured via KVS (`script/heater/set-point`, default 19°C)
- **Home occupancy**: Fetched from myhome daemon API at `http://<mqtt-server>:8889/status`
  - Based on: motion sensors (input events) + mobile device presence (LAN polling)
  - Window: 12 hours
- **No room-level occupancy**: Not implemented
- **No time-based scheduling**: No day/time variations

## Requirements

The heater should use **2 temperature set-points** computed based on:
1. **Time of day** (e.g., night vs. day temperatures)
2. **Day of week** (e.g., weekday vs. weekend schedules)
3. **Home occupancy** (already available via daemon API)
4. **Room occupancy** (per-room Google Calendar)

---

## Option 1: Centralized (Daemon-Based)

### Google Calendar Authentication

**Critical consideration**: Google Calendar API requires OAuth 2.0 authentication.

**Two approaches**:

1. **Service Account** (Recommended for daemon)
   - Create a Google Cloud service account
   - Download JSON credentials file
   - Share calendars with service account email
   - No user interaction required
   - Daemon reads credentials from file
   - **Setup**: One-time configuration per calendar
   - **Cost**: FREE (within Google Calendar API free tier)

2. **OAuth 2.0 User Flow**
   - Requires browser-based authorization
   - Generates refresh token
   - More complex initial setup
   - Better for personal calendars
   - **Cost**: FREE (within Google Calendar API free tier)

**Google Calendar API Pricing**:
- **Free tier**: 1,000,000 requests/day
- **Typical usage**: ~5 requests/minute × 60 min × 24 hours = 7,200 requests/day
- **Verdict**: Well within free tier for home automation use

**Recommended**: Service account approach for automated daemon operation.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     MyHome Daemon                            │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │         Occupancy Service (existing)                   │ │
│  │  - Motion sensors (MQTT events)                        │ │
│  │  - Mobile presence (LAN polling)                       │ │
│  │  - GET /status → {"occupied": bool, "reason": string}  │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │         Temperature Service (NEW)                      │ │
│  │                                                          │ │
│  │  - Google Calendar integration (per-room)              │ │
│  │  - Time/day-based scheduling rules                     │ │
│  │  - Combines: time + day + home + room occupancy        │ │
│  │  - Computes 2 set-points per room                      │ │
│  │                                                          │ │
│  │  GET /temperature/<room-id>                            │ │
│  │  → {                                                    │ │
│  │      "setpoint_comfort": 21.0,                         │ │
│  │      "setpoint_eco": 17.0,                             │ │
│  │      "active_setpoint": 21.0,                          │ │
│  │      "reason": "occupied+daytime",                     │ │
│  │      "schedule": {...}                                 │ │
│  │    }                                                    │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │ HTTP GET
                            │
                    ┌───────┴────────┐
                    │  Shelly Device │
                    │   heater.js    │
                    └────────────────┘
```

### Implementation

#### Daemon Side (Go)

**New package**: `myhome/temperature/`

```go
package temperature

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
    
    "github.com/go-logr/logr"
    "google.golang.org/api/calendar/v3"
    "google.golang.org/api/option"
)

type Service struct {
    ctx              context.Context
    log              logr.Logger
    httpSrv          *http.Server
    calendarService  *calendar.Service
    rooms            map[string]*RoomConfig
}

type RoomConfig struct {
    ID              string
    Name            string
    CalendarID      string        // Google Calendar ID
    ComfortTemp     float64       // Occupied temperature
    EcoTemp         float64       // Unoccupied temperature
    Schedule        *Schedule     // Time-based rules
}

type Schedule struct {
    Weekday   TimeRanges
    Weekend   TimeRanges
}

type TimeRanges struct {
    Comfort []TimeRange  // e.g., [{Start: "06:00", End: "23:00"}]
    Eco     []TimeRange  // e.g., [{Start: "23:00", End: "06:00"}]
}

type TimeRange struct {
    Start string  // "HH:MM"
    End   string  // "HH:MM"
}

type Response struct {
    SetpointComfort  float64 `json:"setpoint_comfort"`
    SetpointEco      float64 `json:"setpoint_eco"`
    ActiveSetpoint   float64 `json:"active_setpoint"`
    Reason           string  `json:"reason"`
    Schedule         any     `json:"schedule,omitempty"`
}

func (s *Service) handleTemperature(w http.ResponseWriter, r *http.Request) {
    roomID := r.PathValue("room")
    
    room, ok := s.rooms[roomID]
    if !ok {
        http.Error(w, "room not found", http.StatusNotFound)
        return
    }
    
    // Check room occupancy from Google Calendar
    roomOccupied := s.isRoomOccupied(room.CalendarID)
    
    // Check time-based schedule
    now := time.Now()
    inComfortHours := room.Schedule.IsComfortTime(now)
    
    // Determine active set-point
    var active float64
    var reason string
    
    if roomOccupied {
        active = room.ComfortTemp
        reason = "room_occupied"
    } else if inComfortHours {
        active = room.ComfortTemp
        reason = "comfort_hours"
    } else {
        active = room.EcoTemp
        reason = "eco_mode"
    }
    
    resp := Response{
        SetpointComfort: room.ComfortTemp,
        SetpointEco:     room.EcoTemp,
        ActiveSetpoint:  active,
        Reason:          reason,
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (s *Service) isRoomOccupied(calendarID string) bool {
    now := time.Now()
    
    events, err := s.calendarService.Events.List(calendarID).
        TimeMin(now.Format(time.RFC3339)).
        TimeMax(now.Add(5 * time.Minute).Format(time.RFC3339)).
        SingleEvents(true).
        Do()
    
    if err != nil {
        s.log.Error(err, "Failed to fetch calendar events", "calendarID", calendarID)
        return false
    }
    
    // Room is occupied if there's an event happening now
    return len(events.Items) > 0
}
```

#### Shelly Script Side (JavaScript)

**Modified `heater.js`**:

```javascript
// Add to CONFIG_SCHEMA
var CONFIG_SCHEMA = {
  // ... existing fields ...
  roomId: {
    description: "Room identifier for temperature API",
    key: "room-id",
    default: "living-room",
    type: "string"
  }
};

// Replace STATE.occupancyUrl with STATE.temperatureUrl
function initUrls() {
  log('initUrls');
  var cfg = Shelly.getComponentConfig('mqtt');
  if (cfg && typeof cfg === 'object') {
    if ("server" in cfg && typeof cfg.server === 'string') {
      var host = cfg.server;
      var i = host.indexOf(':');
      if (i >= 0) host = host.substring(0, i);
      STATE.temperatureUrl = 'http://' + host + ':8890/temperature/' + CONFIG.roomId;
      log('Temperature URL set to', STATE.temperatureUrl);
    }
  }
}

// Replace getOccupancy() with getTemperatureSetpoints()
function getTemperatureSetpoints(cb) {
  log('getTemperatureSetpoints')
  if (!STATE.temperatureUrl) {
    log('Temperature URL not configured, using default setpoint');
    cb({
      setpoint_comfort: CONFIG.setpoint,
      setpoint_eco: CONFIG.setpoint - 2.0,
      active_setpoint: CONFIG.setpoint,
      reason: "no_api"
    });
    return;
  }
  
  Shelly.call("HTTP.GET", {
    url: STATE.temperatureUrl,
    timeout: 5
  }, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.body) {
      var data = null;
      try { data = JSON.parse(result.body); } catch (e) { if (e && false) {} }
      if (data) {
        cb(data);
      } else {
        cb({
          setpoint_comfort: CONFIG.setpoint,
          setpoint_eco: CONFIG.setpoint - 2.0,
          active_setpoint: CONFIG.setpoint,
          reason: "parse_error"
        });
      }
    } else {
      log('Error fetching temperature setpoints:', error_message);
      cb({
        setpoint_comfort: CONFIG.setpoint,
        setpoint_eco: CONFIG.setpoint - 2.0,
        active_setpoint: CONFIG.setpoint,
        reason: "api_error"
      });
    }
  });
}

// Modify controlHeaterWithInputs to use dynamic setpoints
function controlHeaterWithInputs(results) {
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  
  log('Internal:', internalTemp, 'External:', externalTemp, 'Forecast:', forecastTemp);
  
  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }
  
  // Fetch temperature setpoints from daemon
  getTemperatureSetpoints(function(setpoints) {
    log('Setpoints:', JSON.stringify(setpoints));
    
    var targetTemp = setpoints.active_setpoint;
    
    var controlInput = 0;
    var count = 0;
    if (externalTemp !== null) { controlInput += externalTemp; count++; }
    if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
    if (count > 0) controlInput = controlInput / count;
    
    var filteredTemp = kalman.filter(internalTemp, controlInput);
    log('Filtered temperature:', filteredTemp, 'Target:', targetTemp);
    
    var heaterShouldBeOn = filteredTemp < targetTemp;
    
    // SAFETY: Use eco setpoint as minimum threshold
    if (filteredTemp < setpoints.setpoint_eco) {
      log('Safety override: temp below eco setpoint => HEAT');
      setHeaterState(true);
      return;
    }
    
    // Calculate minimum forecast temperature for preheat window
    var mfTemp = getMinForecastTemp(CONFIG.preheatHours);
    
    shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
      // Update shouldPreheat to use targetTemp instead of CONFIG.setpoint
      if ((heaterShouldBeOn && isCheapHour()) || preheat) {
        log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
        setHeaterState(true);
      } else {
        log('Outside cheap window => no heating');
        setHeaterState(false);
      }
    });
  });
}
```

### Pros

✅ **Centralized logic**: All scheduling/occupancy logic in one place (daemon)  
✅ **Easier to maintain**: Single codebase for all rooms  
✅ **Better observability**: Can log/monitor all decisions centrally  
✅ **Reusable**: Multiple Shelly devices can use the same API  
✅ **Powerful**: Full Go ecosystem (Google Calendar API, complex scheduling)  
✅ **Testable**: Easy to unit test scheduling logic  
✅ **Configuration management**: Can use database/config files  
✅ **No script updates**: Schedule changes don't require script re-upload  
✅ **Secure authentication**: Service account with proper OAuth 2.0  
✅ **Granular permissions**: Can share specific calendars with service account  
✅ **Revocable access**: Can revoke service account access anytime  

### Cons

❌ **Network dependency**: Shelly devices must reach daemon API  
❌ **Single point of failure**: If daemon is down, no temperature control  
❌ **Latency**: HTTP round-trip for every control cycle (5 min intervals)  
❌ **More complex daemon**: Adds Google Calendar integration to daemon  
❌ **Initial setup**: Requires Google Cloud project + service account creation  
❌ **Credential management**: Must securely store service account JSON file  

---

## Option 2: Distributed (Shelly-Based)

### Google Calendar Authentication

**Critical limitation**: Shelly devices cannot perform OAuth 2.0 flows.

**Only viable approach**: Public iCal URLs

- Google Calendar → Settings → Integrate calendar → Secret address in iCal format
- Generates public URL: `https://calendar.google.com/calendar/ical/...../basic.ics`
- **No authentication required** (URL contains secret token)
- **Security risk**: Anyone with URL can read calendar
- **Limited control**: Cannot revoke access without regenerating URL
- **Read-only**: Cannot modify events (acceptable for this use case)
- **Cost**: FREE (no API usage, direct iCal download)

**Trade-off**: Convenience vs. security. Public iCal URLs are the only option for Shelly devices.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     MyHome Daemon                            │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │         Occupancy Service (existing)                   │ │
│  │  - Motion sensors (MQTT events)                        │ │
│  │  - Mobile presence (LAN polling)                       │ │
│  │  - GET /status → {"occupied": bool, "reason": string}  │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                            ▲
                            │ HTTP GET (home occupancy only)
                            │
                    ┌───────┴────────┐
                    │  Shelly Device │
                    │   heater.js    │
                    │                │
                    │  - Fetches Google Calendar directly
                    │  - Implements scheduling logic
                    │  - Computes 2 set-points locally
                    └────────────────┘
```

### Implementation

#### Shelly Script (JavaScript)

**Modified `heater.js`**:

```javascript
// Add to CONFIG_SCHEMA
var CONFIG_SCHEMA = {
  // ... existing fields ...
  comfortTemp: {
    description: "Comfort temperature (occupied/daytime)",
    key: "comfort-temp",
    default: 21.0,
    type: "number"
  },
  ecoTemp: {
    description: "Eco temperature (unoccupied/nighttime)",
    key: "eco-temp",
    default: 17.0,
    type: "number"
  },
  calendarUrl: {
    description: "Google Calendar iCal URL for room occupancy",
    key: "calendar-url",
    default: null,
    type: "string"
  },
  comfortStartHour: {
    description: "Start hour for comfort mode (weekdays)",
    key: "comfort-start-hour",
    default: 6,
    type: "number"
  },
  comfortEndHour: {
    description: "End hour for comfort mode (weekdays)",
    key: "comfort-end-hour",
    default: 23,
    type: "number"
  },
  weekendComfortStartHour: {
    description: "Start hour for comfort mode (weekends)",
    key: "weekend-comfort-start-hour",
    default: 8,
    type: "number"
  },
  weekendComfortEndHour: {
    description: "End hour for comfort mode (weekends)",
    key: "weekend-comfort-end-hour",
    default: 23,
    type: "number"
  }
};

// Check if current time is in comfort hours
function isComfortTime() {
  var now = new Date();
  var hour = now.getHours();
  var day = now.getDay(); // 0=Sunday, 6=Saturday
  
  var isWeekend = (day === 0 || day === 6);
  
  if (isWeekend) {
    return (hour >= CONFIG.weekendComfortStartHour && hour < CONFIG.weekendComfortEndHour);
  } else {
    return (hour >= CONFIG.comfortStartHour && hour < CONFIG.comfortEndHour);
  }
}

// Parse iCal format to check if room is occupied now
function isRoomOccupiedFromCalendar(icalData, cb) {
  // Simple iCal parser for VEVENT blocks
  // Note: This is a simplified parser - production would need more robust parsing
  
  if (!icalData || icalData.length === 0) {
    cb(false);
    return;
  }
  
  var now = new Date();
  var lines = icalData.split('\n');
  var inEvent = false;
  var eventStart = null;
  var eventEnd = null;
  
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i].trim();
    
    if (line === 'BEGIN:VEVENT') {
      inEvent = true;
      eventStart = null;
      eventEnd = null;
    } else if (line === 'END:VEVENT') {
      // Check if now is within this event
      if (eventStart && eventEnd) {
        if (now >= eventStart && now <= eventEnd) {
          cb(true);
          return;
        }
      }
      inEvent = false;
    } else if (inEvent) {
      if (line.indexOf('DTSTART') === 0) {
        var startStr = line.split(':')[1];
        eventStart = parseICalDate(startStr);
      } else if (line.indexOf('DTEND') === 0) {
        var endStr = line.split(':')[1];
        eventEnd = parseICalDate(endStr);
      }
    }
  }
  
  cb(false);
}

// Parse iCal date format: 20240315T143000Z
function parseICalDate(str) {
  if (!str || str.length < 15) return null;
  
  var year = parseInt(str.substring(0, 4));
  var month = parseInt(str.substring(4, 6)) - 1;
  var day = parseInt(str.substring(6, 8));
  var hour = parseInt(str.substring(9, 11));
  var minute = parseInt(str.substring(11, 13));
  var second = parseInt(str.substring(13, 15));
  
  return new Date(Date.UTC(year, month, day, hour, minute, second));
}

// Fetch room occupancy from Google Calendar
function getRoomOccupancy(cb) {
  if (!CONFIG.calendarUrl) {
    log('No calendar URL configured, assuming not occupied');
    cb(false);
    return;
  }
  
  Shelly.call("HTTP.GET", {
    url: CONFIG.calendarUrl,
    timeout: 10
  }, function(result, error_code, error_message) {
    if (error_code === 0 && result && result.body) {
      isRoomOccupiedFromCalendar(result.body, cb);
    } else {
      log('Error fetching calendar:', error_message);
      cb(false); // Default: not occupied
    }
  });
}

// Compute active setpoint based on all factors
function computeActiveSetpoint(homeOccupied, roomOccupied, cb) {
  var inComfortHours = isComfortTime();
  var targetTemp;
  var reason;
  
  if (roomOccupied) {
    targetTemp = CONFIG.comfortTemp;
    reason = "room_occupied";
  } else if (homeOccupied && inComfortHours) {
    targetTemp = CONFIG.comfortTemp;
    reason = "home_occupied+comfort_hours";
  } else if (inComfortHours) {
    targetTemp = CONFIG.comfortTemp;
    reason = "comfort_hours";
  } else {
    targetTemp = CONFIG.ecoTemp;
    reason = "eco_mode";
  }
  
  log('Active setpoint:', targetTemp, 'Reason:', reason);
  
  // Break call stack
  Timer.set(100, false, function() {
    cb(targetTemp, reason);
  });
}

// Modified fetchAllControlInputs
function fetchAllControlInputs(cb) {
  if (shouldRefreshForecast()) {
    log('Fetching fresh forecast from Open-Meteo...');
    fetchAndCacheForecast(fetchControlInputsWithOccupancy.bind(null, cb));
  } else {
    log('Using cached forecast');
    fetchControlInputsWithOccupancy(cb);
  }
}

function fetchControlInputsWithOccupancy(cb) {
  log('fetchControlInputsWithOccupancy');
  
  // Fetch home occupancy
  getOccupancy(function(homeOccupied) {
    // Fetch room occupancy
    getRoomOccupancy(function(roomOccupied) {
      // Compute active setpoint
      computeActiveSetpoint(homeOccupied, roomOccupied, function(targetTemp, reason) {
        var results = {
          internal: STATE.temperature['internal'],
          external: STATE.temperature['external'],
          forecast: getCurrentForecastTemp(),
          targetTemp: targetTemp,
          reason: reason
        };
        
        Timer.set(100, false, function() {
          cb(results);
        });
      });
    });
  });
}

// Modified controlHeaterWithInputs
function controlHeaterWithInputs(results) {
  var internalTemp = results.internal;
  var externalTemp = results.external;
  var forecastTemp = results.forecast;
  var targetTemp = results.targetTemp;
  
  log('Internal:', internalTemp, 'External:', externalTemp, 'Forecast:', forecastTemp, 'Target:', targetTemp);
  
  if (internalTemp === null) {
    log('Skipping control cycle due to missing internal temperature');
    return;
  }
  
  var controlInput = 0;
  var count = 0;
  if (externalTemp !== null) { controlInput += externalTemp; count++; }
  if (forecastTemp !== null) { controlInput += forecastTemp; count++; }
  if (count > 0) controlInput = controlInput / count;
  
  var filteredTemp = kalman.filter(internalTemp, controlInput);
  log('Filtered temperature:', filteredTemp, 'Target:', targetTemp);
  
  var heaterShouldBeOn = filteredTemp < targetTemp;
  
  // SAFETY: Use eco temp as minimum threshold
  if (filteredTemp < CONFIG.ecoTemp) {
    log('Safety override: temp below eco setpoint => HEAT');
    setHeaterState(true);
    return;
  }
  
  var mfTemp = getMinForecastTemp(CONFIG.preheatHours);
  
  // Update shouldPreheat to use targetTemp
  shouldPreheat(filteredTemp, forecastTemp, mfTemp, function(preheat) {
    if ((heaterShouldBeOn && isCheapHour()) || preheat) {
      log('Heater ON (normal or preheat mode)', 'preheat:', preheat);
      setHeaterState(true);
    } else {
      log('Outside cheap window => no heating');
      setHeaterState(false);
    }
  });
}
```

### Pros

✅ **Autonomous**: Each device operates independently  
✅ **No daemon dependency**: Works even if daemon is down  
✅ **Lower latency**: No HTTP round-trip to daemon  
✅ **Simpler daemon**: No need to add Google Calendar integration  
✅ **Distributed load**: Each device fetches its own calendar  
✅ **Simple setup**: No Google Cloud project or service account needed  

### Cons

❌ **Code duplication**: Scheduling logic in every script  
❌ **Harder to maintain**: Changes require re-uploading all scripts  
❌ **Limited JavaScript**: Shelly's JS engine is constrained (ES5, no async/await)  
❌ **iCal parsing complexity**: Need to implement iCal parser in ES5 JavaScript  
❌ **Security risk**: Must use public iCal URLs (anyone with URL can read calendar)  
❌ **No access control**: Cannot revoke access without regenerating URL (breaks all devices)  
❌ **URL management**: Must configure secret URLs in KVS on every device  
❌ **Testing difficulty**: Hard to unit test JavaScript on Shelly  
❌ **Resource constraints**: Multiple HTTP calls per control cycle (occupancy + calendar + forecast)  
❌ **No centralized monitoring**: Can't easily see what all devices are doing  
❌ **Credential exposure**: iCal URLs stored in device KVS (visible via Shelly API)  

---

## Recommendation

**Option 1 (Centralized/Daemon-Based)** is strongly recommended for the following reasons:

### Technical Superiority

1. **Maintainability**: Single source of truth for scheduling logic
2. **Testability**: Can write proper unit tests in Go
3. **Flexibility**: Easy to add complex rules (holidays, vacation mode, etc.)
4. **Observability**: Centralized logging and monitoring
5. **Performance**: Google Calendar API is more efficient than iCal parsing
6. **Reliability**: Proper error handling and retry logic in Go

### Practical Considerations

1. **Network dependency is acceptable**: 
   - Daemon already required for home occupancy
   - 5-minute polling interval means latency is not critical
   - Can implement fallback to static setpoints if API fails

2. **Single point of failure is mitigated**:
   - Shelly scripts can fall back to static setpoints
   - Daemon is already critical infrastructure (MQTT broker, device manager)
   - Can implement health checks and automatic restarts

3. **Development velocity**:
   - Much faster to iterate on Go code than Shelly scripts
   - No need to re-upload scripts for schedule changes
   - Can hot-reload configuration without device restarts

4. **Security advantage**:
   - **Option 1**: Service account credentials stored securely on daemon host (one location)
   - **Option 2**: Public iCal URLs stored in KVS on every Shelly device (N locations, visible via API)
   - **Option 1**: Can revoke access per calendar without breaking other rooms
   - **Option 2**: Revoking access requires regenerating URL and updating all devices
   - **Option 1**: Proper OAuth 2.0 with granular permissions
   - **Option 2**: Secret URLs that anyone can use if leaked

### Authentication Setup (Option 1)

**Step 1: Create Google Cloud Project**
```bash
# 1. Go to https://console.cloud.google.com
# 2. Create new project: "MyHome Automation"
# 3. Enable Google Calendar API
```

**Step 2: Create Service Account**
```bash
# 1. IAM & Admin → Service Accounts → Create Service Account
# 2. Name: "myhome-calendar-reader"
# 3. Grant role: None (calendar permissions via sharing)
# 4. Create key → JSON → Download credentials.json
```

**Step 3: Share Calendars**
```bash
# 1. Open each room calendar in Google Calendar
# 2. Settings → Share with specific people
# 3. Add service account email: myhome-calendar-reader@PROJECT_ID.iam.gserviceaccount.com
# 4. Permission: "See all event details" (read-only)
```

**Step 4: Configure Daemon**
```yaml
temperature_service:
  google_credentials_file: "/etc/myhome/google-credentials.json"
  # ... rest of config
```

**Security**: 
- Store credentials file outside web root
- Set file permissions: `chmod 600 /etc/myhome/google-credentials.json`
- Service account has no access to other Google services
- Can revoke access per calendar anytime

### Implementation Path

**Phase 0: Authentication Setup**
- Create Google Cloud project + service account
- Share calendars with service account
- Store credentials securely on daemon host

**Phase 1: Basic API (No Calendar)**
- Add `/temperature/<room-id>` endpoint to daemon
- Implement time-based scheduling (day/night, weekday/weekend)
- Update `heater.js` to consume API
- Configure via KVS or config file

**Phase 2: Google Calendar Integration**
- Add Google Calendar API client with service account auth
- Implement per-room calendar checking
- Add calendar-based occupancy to decision logic

**Phase 3: Advanced Features**
- Holiday detection
- Vacation mode
- Learning algorithms (optimal comfort hours)
- Multi-zone coordination

---

## Configuration Example

### Daemon Configuration (YAML)

```yaml
# Eco is the default - only define comfort hours
temperature_service:
  port: 8890
  rooms:
    living-room:
      name: "Living Room"
      calendar_id: "abc123@group.calendar.google.com"
      comfort_temp: 21.0
      eco_temp: 17.0
      schedule:
        weekday: ["06:00-23:00"]
        weekend: ["08:00-23:00"]
    
    bedroom:
      name: "Bedroom"
      calendar_id: "xyz789@group.calendar.google.com"
      comfort_temp: 19.0
      eco_temp: 16.0
      schedule:
        weekday: ["06:00-08:00", "20:00-23:00"]
        weekend: ["08:00-23:00"]
```

### Shelly Configuration (KVS)

```bash
# Configure room ID for this heater
myhome ctl shelly kvs set heater-living script/heater/room-id living-room

# Temperature topics (unchanged)
myhome ctl shelly kvs set heater-living script/heater/internal-temperature-topic "shelly-ht-living/events/rpc"
myhome ctl shelly kvs set heater-living script/heater/external-temperature-topic "shelly-ht-outdoor/events/rpc"

# Cheap electricity window (unchanged)
myhome ctl shelly kvs set heater-living script/heater/cheap-start-hour 23
myhome ctl shelly kvs set heater-living script/heater/cheap-end-hour 7
```

---

## Authentication Comparison Summary

| Aspect | Option 1 (Daemon) | Option 2 (Shelly) |
|--------|-------------------|-------------------|
| **Authentication Method** | OAuth 2.0 Service Account | Public iCal URLs |
| **Initial Setup** | Google Cloud project + service account | Generate secret iCal URLs |
| **Credential Storage** | Single file on daemon host | KVS on every device |
| **Security** | ✅ Proper OAuth with granular permissions | ⚠️ Secret URLs (anyone with URL can read) |
| **Access Control** | ✅ Per-calendar revocation | ❌ All-or-nothing (regenerate URL) |
| **Credential Exposure** | ✅ One secure location | ❌ N devices, visible via Shelly API |
| **Setup Complexity** | Medium (one-time per project) | Low (per calendar) |
| **Maintenance** | ✅ Centralized credential rotation | ❌ Update all devices on URL change |
| **API Features** | ✅ Full Calendar API (queries, filters) | ⚠️ Limited iCal format (parse manually) |
| **Cost** | ✅ FREE (1M requests/day free tier) | ✅ FREE (no API usage) |

**Verdict**: Both options are FREE. Option 1 provides significantly better security and maintainability for authentication.

---

## Cost Analysis

### Option 1: Google Calendar API (via GCP)

**Google Cloud Platform Account**: FREE
- No credit card required for Calendar API usage
- No charges for staying within free tier

**Google Calendar API Free Tier**:
- **Quota**: 1,000,000 requests per day
- **Typical home automation usage**:
  - 1 request per room every 5 minutes
  - 3 rooms × 12 requests/hour × 24 hours = **864 requests/day**
  - **0.086% of free tier quota**

**Cost breakdown**:
- GCP account creation: $0
- Service account: $0
- Calendar API calls: $0 (well within free tier)
- **Total monthly cost: $0**

**Paid tier** (if you somehow exceed 1M requests/day):
- $0.25 per 1,000 requests beyond free tier
- Would need 1,157 requests/minute to exceed free tier
- **Not realistic for home automation**

### Option 2: Public iCal URLs

**Cost**: $0
- No Google Cloud account needed
- No API usage (direct HTTP download)
- No quotas or limits

**Trade-off**: Zero cost, but significantly worse security model

### Verdict

**Both options are completely FREE for home automation use cases.**

The Google Calendar API free tier (1M requests/day) is so generous that even with dozens of rooms checking every minute, you'd still be at <1% of the quota. There is **no cost disadvantage** to Option 1.

---

## Setup Complexity Comparison

### Option 1: Daemon-Based Setup

**One-time setup** (15-20 minutes):

1. **Create Google Cloud Project** (5 min)
   - Go to https://console.cloud.google.com
   - Click "Create Project" → Name: "MyHome Automation"
   - Enable Google Calendar API (click "Enable APIs")

2. **Create Service Account** (3 min)
   - IAM & Admin → Service Accounts → Create
   - Name: "myhome-calendar-reader"
   - Create key (JSON) → Download to daemon host
   - Note service account email: `myhome-calendar-reader@PROJECT_ID.iam.gserviceaccount.com`

3. **Create Room Calendars** (2 min per room)
   - Google Calendar → Create new calendar
   - Name: "Living Room Occupancy" (or "Bedroom Occupancy", etc.)
   - Description: "Room occupancy schedule for heating control"
   - **Share with service account**: Add `myhome-calendar-reader@...` with "See all event details" permission

4. **Configure Daemon** (5 min)
   - Copy credentials JSON to daemon host: `/etc/myhome/google-credentials.json`
   - Set permissions: `chmod 600 /etc/myhome/google-credentials.json`
   - Add room configs to YAML (calendar IDs, temperatures, schedules)
   - Restart daemon

**Per-room setup** (2 min):
- Create calendar for room
- Share with service account
- Add calendar ID to daemon config
- Configure heater device with room ID via KVS

**Managing schedules**:
- ✅ **Easy**: Add events in Google Calendar (web/mobile app)
- ✅ **Visual**: See schedule at a glance
- ✅ **Flexible**: Recurring events, exceptions, one-time events
- ✅ **No device updates**: Changes take effect immediately (next API poll)

**Example calendar events**:
- "Working from home" (Mon-Fri 9am-5pm, recurring)
- "Vacation" (Dec 20-Jan 5, all day)
- "Guest staying" (specific dates)

### Option 2: Shelly-Based Setup

**One-time setup** (5 min):
- No Google Cloud project needed
- No service account needed

**Per-room setup** (5-10 min per room):

1. **Create Room Calendar** (2 min)
   - Google Calendar → Create new calendar
   - Name: "Living Room Occupancy"

2. **Generate iCal URL** (2 min)
   - Calendar Settings → Integrate calendar
   - Copy "Secret address in iCal format"
   - URL looks like: `https://calendar.google.com/calendar/ical/abc123...xyz/basic.ics`

3. **Configure Each Heater Device** (3 min per device)
   - Set iCal URL in KVS: `myhome ctl shelly kvs set heater-living script/heater/calendar-url "https://..."`
   - Set comfort/eco temperatures
   - Set time schedules (weekday/weekend hours)

4. **Update Script** (one-time, 5 min)
   - Upload modified heater.js with calendar parsing logic
   - Test iCal parsing

**Managing schedules**:
- ✅ **Easy**: Add events in Google Calendar
- ⚠️ **Delayed**: iCal updates can take 5-15 minutes to propagate
- ⚠️ **Limited**: Must parse iCal format in JavaScript
- ❌ **Security risk**: Secret URL visible in device KVS

**If iCal URL needs to be regenerated**:
- Must update KVS on ALL devices using that calendar
- No way to revoke old URL

### Setup Complexity Verdict

| Aspect | Option 1 (Daemon) | Option 2 (Shelly) |
|--------|-------------------|-------------------|
| **Initial setup time** | 15-20 min (one-time) | 5 min (one-time) |
| **Per-room setup** | 2 min | 5-10 min |
| **Total for 3 rooms** | ~25 min | ~20-35 min |
| **Calendar creation** | Same (Google Calendar) | Same (Google Calendar) |
| **Schedule updates** | ✅ Instant (via Calendar app) | ⚠️ 5-15 min delay (iCal sync) |
| **Device configuration** | ✅ One-time (room ID only) | ❌ Per-device (URLs, schedules) |
| **Script updates** | ✅ Never (logic in daemon) | ❌ Every time (logic in script) |
| **URL regeneration** | N/A | ❌ Update all devices |

**Key insight**: Option 1 has higher **initial** setup cost (15 min), but **lower ongoing** maintenance. Option 2 has lower initial setup but **higher per-room and maintenance** costs.

For 3+ rooms, **Option 1 becomes easier** overall.

---

## Calendar Creation & Usage Guide

### Creating Room Calendars (Both Options)

**Recommended approach**: One calendar per room

1. **Go to Google Calendar** (https://calendar.google.com)

2. **Create new calendar**:
   - Click "+" next to "Other calendars"
   - Select "Create new calendar"
   - **Name**: `<Room Name> Occupancy` (e.g., "Living Room Occupancy")
   - **Description**: "Heating schedule for <room name>"
   - **Time zone**: Your local time zone
   - Click "Create calendar"

3. **Add occupancy events**:
   
   **Example: Working from home**
   - Title: "Working from home"
   - Date: Recurring (Mon-Fri)
   - Time: 9:00 AM - 5:00 PM
   - Repeat: Weekly on weekdays
   
   **Example: Guest bedroom**
   - Title: "Guest staying"
   - Date: Dec 20-25
   - Time: All day
   - Repeat: Does not repeat
   
   **Example: Yoga class**
   - Title: "Yoga"
   - Date: Recurring (Tue, Thu)
   - Time: 6:00 PM - 7:30 PM
   - Repeat: Weekly

4. **Get Calendar ID** (for Option 1):
   - Calendar Settings → Integrate calendar
   - **Calendar ID**: `abc123xyz@group.calendar.google.com`
   - Copy this for daemon config

5. **Share with service account** (Option 1 only):
   - Calendar Settings → Share with specific people
   - Add: `myhome-calendar-reader@PROJECT_ID.iam.gserviceaccount.com`
   - Permission: "See all event details"
   - Uncheck "Make available to public"

### How It Works

**Occupancy detection logic**:
- If there's an event happening NOW → Room is occupied → Use comfort temperature
- If no event happening → Room is unoccupied → Use time-based schedule or eco temperature

**Combined with time-based rules**:
- Occupied (calendar event) → Always comfort temp
- Unoccupied + comfort hours (e.g., 6am-11pm) → Comfort temp
- Unoccupied + eco hours (e.g., 11pm-6am) → Eco temp

**Example scenarios**:

| Time | Calendar Event | Time Schedule | Result | Temp |
|------|----------------|---------------|--------|------|
| Mon 10am | "Working from home" | Comfort hours | Occupied | 21°C |
| Mon 2pm | (none) | Comfort hours | Unoccupied + comfort | 21°C |
| Mon 11pm | (none) | Eco hours | Unoccupied + eco | 17°C |
| Sat 10am | (none) | Comfort hours | Unoccupied + comfort | 21°C |
| Sat 10am | "Guest staying" | Comfort hours | Occupied | 21°C |

---

## Next Steps

1. **Create `myhome/temperature/` package** with basic time-based scheduling
2. **Add `/temperature/<room-id>` HTTP endpoint** to daemon
3. **Update `heater.js`** to fetch setpoints from API (with fallback)
4. **Test with single room** (living room)
5. **Add Google Calendar integration** (Phase 2)
6. **Roll out to all rooms** (Phase 3)
