// classify-events reads raw Shelly MQTT event files, picks one representative
// per unique (method, component, device-type) shape, writes them as test fixtures,
// then deletes all originals.
//
// Usage (from repo root):
//
//	go run ./tools/classify-events [events-dir] [testdata-dir]
//
// Defaults: myhome/events  →  pkg/shelly/mqtt/testdata
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
)

type rawEvent struct {
	Src    string          `json:"src"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type notifyEventParams struct {
	Events []struct {
		Component string `json:"component"`
		Event     string `json:"event"`
	} `json:"events"`
}

func main() {
	eventsDir := "myhome/events"
	testdataDir := "pkg/shelly/mqtt/testdata"
	if len(os.Args) > 1 {
		eventsDir = os.Args[1]
	}
	if len(os.Args) > 2 {
		testdataDir = os.Args[2]
	}

	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		fatalf("read events dir %s: %v", eventsDir, err)
	}

	type fileRecord struct {
		path string
		key  string
	}

	// first pass: classify every file
	var records []fileRecord
	representatives := map[string]string{} // key → first file path

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(eventsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: read %s: %v\n", path, err)
			continue
		}
		var e rawEvent
		if err := json.Unmarshal(data, &e); err != nil {
			fmt.Fprintf(os.Stderr, "warn: parse %s: %v\n", path, err)
			continue
		}
		key := classify(e)
		records = append(records, fileRecord{path: path, key: key})
		if _, seen := representatives[key]; !seen {
			representatives[key] = path
		}
	}

	if len(records) == 0 {
		fmt.Println("No event files found.")
		return
	}

	// second pass: write representatives to testdata
	if err := os.MkdirAll(testdataDir, 0o755); err != nil {
		fatalf("create testdata dir: %v", err)
	}

	written := 0
	for key, srcPath := range representatives {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			fatalf("read representative %s: %v", srcPath, err)
		}
		// pretty-print for readability
		var raw any
		if err := json.Unmarshal(data, &raw); err != nil {
			fatalf("unmarshal %s: %v", srcPath, err)
		}
		pretty, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			fatalf("marshal %s: %v", srcPath, err)
		}
		dest := filepath.Join(testdataDir, key+".json")
		if err := os.WriteFile(dest, append(pretty, '\n'), 0o644); err != nil {
			fatalf("write %s: %v", dest, err)
		}
		written++
	}

	// third pass: delete all originals
	deleted := 0
	for _, rec := range records {
		if err := os.Remove(rec.path); err != nil {
			fmt.Fprintf(os.Stderr, "warn: delete %s: %v\n", rec.path, err)
		} else {
			deleted++
		}
	}

	// summary
	fmt.Printf("\nWrote %d test fixtures to %s\n", written, testdataDir)
	fmt.Printf("Deleted %d original event files from %s\n\n", deleted, eventsDir)

	// count by key
	counts := map[string]int{}
	for _, rec := range records {
		counts[rec.key]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIXTURE\tORIGINAL COUNT")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s.json\t%d\n", k, counts[k])
	}
	tw.Flush()
}

// classify returns a stable, filesystem-safe key for an event.
func classify(e rawEvent) string {
	deviceType := e.Src
	if idx := strings.Index(deviceType, "-"); idx != -1 {
		deviceType = deviceType[:idx]
	}

	switch e.Method {
	case "NotifyStatus", "NotifyFullStatus":
		var params map[string]json.RawMessage
		if err := json.Unmarshal(e.Params, &params); err == nil {
			for k := range params {
				if k == "ts" {
					continue
				}
				return fmt.Sprintf("notify_status__%s__%s", deviceType, sanitize(k))
			}
		}
		return fmt.Sprintf("notify_status__%s__unknown", deviceType)

	case "NotifyEvent":
		var p notifyEventParams
		if err := json.Unmarshal(e.Params, &p); err == nil && len(p.Events) > 0 {
			ev := p.Events[0]
			return fmt.Sprintf("notify_event__%s__%s__%s",
				deviceType, sanitize(ev.Component), sanitize(ev.Event))
		}
		return fmt.Sprintf("notify_event__%s__unknown", deviceType)
	}

	return fmt.Sprintf("unknown__%s__%s", deviceType, sanitize(e.Method))
}

func sanitize(s string) string {
	r := strings.NewReplacer(":", "_", "-", "_", "/", "_", " ", "_")
	return r.Replace(s)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
