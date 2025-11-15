# Scripts

## heater.js

Example of **device.json** for the script **heater.js**:

```json
{
  "kvs": {
    "normally-closed": true,
    "script/heater/cheap-end-hour": 7,
    "script/heater/cheap-start-hour": 23,
    "script/heater/enable-logging": true,
    "script/heater/external-temperature-topic": "shellies/shellyht-EE45E9/sensor/temperature",
    "script/heater/internal-temperature-topic": "shellies/shellyht-208500/sensor/temperature",
    "script/heater/min-internal-temp": 12,
    "script/heater/poll-interval-ms": 300000,
    "script/heater/preheat-hours": 2,
    "script/heater/set-point": 18.5
  },
  "storage": {
    "cooling-rate": null,
    "external-temp": null,
    "forecast-url": "https://api.open-meteo.com/v1/forecast?latitude=52.52\u0026longitude=13.405\u0026hourly=temperature_2m\u0026forecast_days=1\u0026timezone=auto",
    "internal-temp": 20.12,
    "last-cheap-end": null
  }
}
```