// prometheus-metrics.js - Prometheus metrics exporter for Shelly devices
// Publishes device metrics in Prometheus format via MQTT at a regular interval.

var CONFIG = {
    scriptName: "prometheus-metrics",
    enableLogging: true,
    prometheus: {
        enabled: true,
        publishIntervalSeconds: 30,
        mqttTopic: "shelly/metrics",
        publishIndividualMetrics: true
    }
};

function log() {
    if (!CONFIG.enableLogging) return;
    var s = "";
    for (var i = 0; i < arguments.length; i++) {
        try {
            var a = arguments[i];
            if (typeof a === "object") {
                s += JSON.stringify(a);
            } else {
                s += String(a);
            }
        } catch (e) {
            s += String(arguments[i]);
            if (e && false) {}
        }
        if (i + 1 < arguments.length) s += " ";
    }
    print(CONFIG.scriptName + ": " + s);
}

var PrometheusMetrics = {
    // Constants
    TYPE_GAUGE: "gauge",
    TYPE_COUNTER: "counter",

    // Device info
    deviceInfo: null,
    defaultLabelsStr: "",
    metricPrefix: "shelly_",
    emittedMeta: {},
    publishTimer: null,

    // Track switch states for activation/deactivation events
    monitoredSwitches: [],
    switchStates: {},
    activationCounts: {},
    deactivationCounts: {},

    log: log.bind(this, "[PrometheusMetrics]"),

    // Discover and initialize switch state tracking
    initSwitchTracking: function() {
        this.log("Discovering available switches...");

        var availableSwitches = [];
        for (var i = 0; i < 4; i++) {
            var switchId = "switch:" + i;
            var status = Shelly.getComponentStatus(switchId);
            if (status && ("output" in status)) {
                availableSwitches.push(switchId);
                this.switchStates[switchId] = status.output || false;
                this.activationCounts[switchId] = 0;
                this.deactivationCounts[switchId] = 0;
            }
        }

        this.monitoredSwitches = availableSwitches;
        this.log("Found " + availableSwitches.length + " switch(es): " + JSON.stringify(availableSwitches));
    },

    // Check for switch state changes and track activation/deactivation
    checkSwitchStateChanges: function() {
        var list = this.monitoredSwitches;
        for (var i = 0; i < list.length; i++) {
            var switchId = list[i];
            var sw = Shelly.getComponentStatus(switchId);
            if (!sw) continue;

            var currentState = sw.output || false;
            var previousState = this.switchStates[switchId];

            if (currentState !== previousState) {
                if (currentState) {
                    this.activationCounts[switchId]++;
                    this.log("Switch " + switchId + " activated (count: " + this.activationCounts[switchId] + ")");
                } else {
                    this.deactivationCounts[switchId]++;
                    this.log("Switch " + switchId + " deactivated (count: " + this.deactivationCounts[switchId] + ")");
                }

                this.switchStates[switchId] = currentState;

                if (CONFIG.prometheus.publishIndividualMetrics) {
                    var idNum = switchId.split(":")[1] || "0";
                    this.publishIndividualMetric("switch_" + idNum + "_activated", this.activationCounts[switchId]);
                    this.publishIndividualMetric("switch_" + idNum + "_deactivated", this.deactivationCounts[switchId]);
                }
            }
        }
    },

    // Initialize Prometheus metrics
    init: function() {
        this.log("Initializing metrics");

        if (!CONFIG.prometheus || !CONFIG.prometheus.enabled) {
            this.log("Prometheus metrics are disabled in configuration");
            return;
        }

        try {
            this.deviceInfo = Shelly.getDeviceInfo();

            this.defaultLabelsStr = this.promLabel("name", this.deviceInfo.name) + "," +
                                   this.promLabel("id", this.deviceInfo.id) + "," +
                                   this.promLabel("mac", this.deviceInfo.mac) + "," +
                                   this.promLabel("app", this.deviceInfo.app);
            this.emittedMeta = {};

            this.initSwitchTracking();

            var intervalMs = CONFIG.prometheus.publishIntervalSeconds * 1000;
            this.log("Starting metrics publisher (interval: " + CONFIG.prometheus.publishIntervalSeconds + "s)");

            var self = this;
            this.publishTimer = Timer.set(intervalMs, true, function() {
                self.checkSwitchStateChanges();
                self.publishMetrics();
            });

            this.publishMetrics();

        } catch (e) {
            this.log("Error while initializing Prometheus metrics: " + e.message);
        }

        this.log("Metrics initialized");
    },

    // Create a Prometheus label
    promLabel: function(label, value) {
        return [label, "=", '"', value, '"'].join("");
    },

    // Generate one metric using string concatenation
    printPrometheusMetric: function(name, type, specificLabels, description, value) {
        var labels = this.defaultLabelsStr;
        if (specificLabels && specificLabels.length > 0) {
            labels = labels + "," + specificLabels.join(",");
        }

        var result = "";
        if (!this.emittedMeta[name]) {
            result += "# HELP " + this.metricPrefix + name + " " + description + "\n";
            result += "# TYPE " + this.metricPrefix + name + " " + type + "\n";
            this.emittedMeta[name] = true;
        }

        result += this.metricPrefix + name + "{" + labels + "} " + String(value) + "\n\n";
        return result;
    },

    // Publish individual metric as JSON to MQTT topic
    publishIndividualMetric: function(metricName, value) {
        if (!CONFIG.prometheus.publishIndividualMetrics) {
            return;
        }

        try {
            var topic = "shelly/" + this.deviceInfo.id + "/" + metricName;
            var payload = JSON.stringify({value: value});
            MQTT.publish(topic, payload, 1, false);
        } catch (e) {
            this.log("Error publishing individual metric " + metricName + ": " + e.message);
            if (e && false) {}
        }
    },

    // Publish metrics to MQTT
    publishMetrics: function() {
        if (!MQTT.isConnected()) {
            this.log("MQTT not connected, skipping metrics publish");
            return;
        }

        try {
            this.emittedMeta = {};

            var metrics = this.generateMetricsForSystem() + this.generateMetricsForSwitches();

            var topic = CONFIG.prometheus.mqttTopic + "/" + this.deviceInfo.id;
            MQTT.publish(topic, metrics, 1, false);

            this.log("Published metrics to " + topic);
        } catch (e) {
            this.log("Error publishing metrics: " + e.message);
            if (e && false) {}
        }
    },

    // Generate metrics for the system
    generateMetricsForSystem: function() {
        var sys = Shelly.getComponentStatus("sys");
        var result = "";
        result += this.printPrometheusMetric("uptime_seconds", this.TYPE_COUNTER, [], "System uptime in seconds", sys.uptime);
        result += this.printPrometheusMetric("ram_size_bytes", this.TYPE_GAUGE, [], "Internal board RAM size in bytes", sys.ram_size);
        result += this.printPrometheusMetric("ram_free_bytes", this.TYPE_GAUGE, [], "Internal board free RAM size in bytes", sys.ram_free);

        this.publishIndividualMetric("uptime_seconds", sys.uptime);
        this.publishIndividualMetric("ram_size_bytes", sys.ram_size);
        this.publishIndividualMetric("ram_free_bytes", sys.ram_free);

        return result;
    },

    // Generate metrics for all monitored switches
    generateMetricsForSwitches: function() {
        var list = this.monitoredSwitches;
        var result = "";
        for (var i = 0; i < list.length; i++) {
            var switchId = list[i];
            var id = switchId.split(":")[1] || "0";
            result += this.generateMetricsForSwitch(id);
        }
        return result;
    },

    // Build Prometheus metric lines for a switch component.
    // Extracted to keep generateMetricsForSwitch to a single return point,
    // preventing the minifier from producing a deep ternary-in-return expression
    // that overflows Espruino's C expression-evaluator stack.
    _buildSwitchMetrics: function(id, sw, switchLabel) {
        var n = "";
        n += this.printPrometheusMetric("switch_power_watts", this.TYPE_GAUGE, [switchLabel], "Instant power consumption in watts", sw.apower || 0);
        n += this.printPrometheusMetric("switch_voltage_volts", this.TYPE_GAUGE, [switchLabel], "Instant voltage in volts", sw.voltage || 0);
        n += this.printPrometheusMetric("switch_current_amperes", this.TYPE_GAUGE, [switchLabel], "Instant current in amperes", sw.current || 0);
        n += this.printPrometheusMetric("switch_temperature_celsius", this.TYPE_GAUGE, [switchLabel], "Temperature of the device in celsius", (sw.temperature && sw.temperature.tC) ? sw.temperature.tC : 0);
        n += this.printPrometheusMetric("switch_power_total", this.TYPE_COUNTER, [switchLabel], "Accumulated energy consumed in watts hours", (sw.aenergy && sw.aenergy.total) ? sw.aenergy.total : 0);
        n += this.printPrometheusMetric("switch_output", this.TYPE_GAUGE, [switchLabel], "Switch state (1=on, 0=off)", sw.output ? 1 : 0);
        return n;
    },

    // Publish individual MQTT metrics for a switch component.
    _publishSwitchMetrics: function(id, sw) {
        this.publishIndividualMetric("switch_" + id + "_power_watts", sw.apower || 0);
        this.publishIndividualMetric("switch_" + id + "_voltage_volts", sw.voltage || 0);
        this.publishIndividualMetric("switch_" + id + "_current_amperes", sw.current || 0);
        this.publishIndividualMetric("switch_" + id + "_temperature_celsius", (sw.temperature && sw.temperature.tC) ? sw.temperature.tC : 0);
        this.publishIndividualMetric("switch_" + id + "_power_total", (sw.aenergy && sw.aenergy.total) ? sw.aenergy.total : 0);
        this.publishIndividualMetric("switch_" + id + "_output", sw.output ? 1 : 0);
    },

    // Generate metrics for a specific switch.
    // Single return outside the try-catch: the minifier cannot produce
    // "return cond ? (N-element-comma) : fallback" because try-catch is not
    // a JS expression and blocks statement merging into the return.
    generateMetricsForSwitch: function(id) {
        var result = "";
        try {
            var stringId = "switch:" + id;
            var sw = Shelly.getComponentStatus(stringId);
            if (sw) {
                var switchLabel = this.promLabel("switch", stringId);
                result = this._buildSwitchMetrics(id, sw, switchLabel);
                this._publishSwitchMetrics(id, sw);
            }
        } catch (e) {
            if (e && false) {}
            result = "";
        }
        return result;
    }
};

// Initialize all components (wrapped to prevent minifier collapsing into comma sequences)
(function() {
    print("Script starting...");

    PrometheusMetrics.init();

    print("Script startup complete");
})();
