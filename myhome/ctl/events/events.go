package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	myevents "github.com/asnowfix/home-automation/myhome/events"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// Cmd is the root "events" sub-command registered under "myhome ctl".
var Cmd = &cobra.Command{
	Use:   "events",
	Short: "Query and follow the device event log",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(followCmd)
	Cmd.AddCommand(clearCmd)
}

// ============================================================================
// list
// ============================================================================

var (
	listDevice   string
	listType     string
	listSeverity string
	listSince    string
	listLimit    int
	listJSON     bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recorded events",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		since := 24 * time.Hour
		if listSince != "" {
			d, err := time.ParseDuration(listSince)
			if err != nil {
				return fmt.Errorf("invalid --since value %q: %w", listSince, err)
			}
			since = d
		}

		req := &myhome.EventListRequest{
			DeviceID:  listDevice,
			EventType: listType,
			Severity:  listSeverity,
			Since:     since,
			Limit:     listLimit,
		}

		result, err := myhome.TheClient.CallE(ctx, myhome.EventList, req)
		if err != nil {
			return err
		}

		resp, ok := result.(*myhome.EventListResponse)
		if !ok {
			return fmt.Errorf("unexpected result type: %T", result)
		}

		if listJSON || options.Flags.Json {
			out, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}

		if len(resp.Events) == 0 {
			fmt.Println("No events found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tDEVICE\tCOMPONENT\tEVENT\tSEVERITY\tDATA")
		fmt.Fprintln(w, "----\t------\t---------\t-----\t--------\t----")
		for _, e := range resp.Events {
			ts := time.Unix(int64(e.Ts), 0).Format("2006-01-02 15:04:05")
			data := ""
			if e.Data != nil {
				data = *e.Data
				if len(data) > 60 {
					data = data[:57] + "..."
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				ts, e.DeviceID, e.Component, e.Event, e.Severity, data)
		}
		w.Flush()
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listDevice, "device", "", "Filter by device ID/name/MAC (default: all)")
	listCmd.Flags().StringVar(&listType, "type", "", "Event name prefix, e.g. \"switch\" (default: all)")
	listCmd.Flags().StringVar(&listSeverity, "severity", "", "Filter by severity: alarm|warn|notice|info|debug (default: all)")
	listCmd.Flags().StringVar(&listSince, "since", "24h", "Show events since this duration ago, e.g. 24h, 7d (default: 24h)")
	listCmd.Flags().IntVar(&listLimit, "limit", 100, "Maximum number of rows to return (default: 100)")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output JSON instead of a table")
}

// ============================================================================
// follow — tail live events via SSE
// ============================================================================

var (
	followDevice   string
	followType     string
	followSeverity string
)

// severityRank maps severity labels to numeric rank (higher = more severe).
// "notice" is not a severity escalation per se — it curates events worth a
// human's attention — but it is ranked above "info" so that
// `events follow --severity notice` surfaces notices and anything more
// severe, without requiring a separate filtering dimension.
var severityRank = map[string]int{
	"debug":  0,
	"info":   1,
	"notice": 2,
	"warn":   3,
	"alarm":  4,
}

var followCmd = &cobra.Command{
	Use:   "follow",
	Short: "Stream live events from the running daemon via SSE",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		host := "localhost"
		port := options.Flags.UiPort
		if port == 0 {
			port = options.HTTP_DEFAULT_PORT
		}
		url := fmt.Sprintf("http://%s:%d/events", host, port)

		minRank := severityRank["info"]
		if followSeverity != "" {
			r, ok := severityRank[strings.ToLower(followSeverity)]
			if !ok {
				return fmt.Errorf("invalid --severity %q: must be debug|info|notice|warn|alarm", followSeverity)
			}
			minRank = r
		}

		backoff := time.Second
		const maxBackoff = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			if err := followSSE(ctx, url, followDevice, followType, minRank); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(os.Stderr, "SSE disconnected (%v), reconnecting in %s\n", err, backoff)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			} else {
				backoff = time.Second
			}
		}
	},
}

func followSSE(ctx context.Context, url, deviceFilter, typeFilter string, minRank int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	var currentEvent string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			currentEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if currentEvent != "eventlog" {
				continue
			}
			rawData := strings.TrimPrefix(line, "data: ")
			var ev myhome.EventView
			if err := json.Unmarshal([]byte(rawData), &ev); err != nil {
				continue
			}
			// Client-side filters
			if deviceFilter != "" && ev.DeviceID != deviceFilter {
				continue
			}
			if typeFilter != "" && !strings.HasPrefix(ev.Event, typeFilter) {
				continue
			}
			rank, ok := severityRank[strings.ToLower(ev.Severity)]
			if ok && rank < minRank {
				continue
			}
			ts := time.Unix(int64(ev.Ts), 0).Format("2006-01-02 15:04:05")
			data := ""
			if ev.Data != nil {
				data = " " + *ev.Data
			}
			fmt.Printf("%s %s %s %s%s\n", ts, ev.DeviceID, ev.Component, ev.Event, data)
		case line == "":
			currentEvent = ""
		}
	}
	return scanner.Err()
}

func init() {
	followCmd.Flags().StringVar(&followDevice, "device", "", "Filter by device ID/name/MAC (default: all)")
	followCmd.Flags().StringVar(&followType, "type", "", "Event name prefix filter, e.g. \"switch\" (default: all)")
	followCmd.Flags().StringVar(&followSeverity, "severity", "info", "Minimum severity: debug|info|notice|warn|alarm (default: info)")
}

// ============================================================================
// clear — purge old events directly from the DB
// ============================================================================

var (
	clearBefore string
	clearDryRun bool
)

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Purge old events from the events database",
	Long: `Purge events older than the given cutoff directly from the events SQLite database.
Does not require the daemon to be running.`,
	// Override PersistentPreRunE from ctl.go: clear reads the DB directly and
	// does not need an MQTT connection.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := options.Flags.EventsDBPath
		if dbPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			dbPath = home + "/.myhome/events.db"
		}

		var cutoff time.Time
		if clearBefore == "" {
			retention := options.Flags.EventsRetention
			if retention == 0 {
				retention = 90 * 24 * time.Hour
			}
			cutoff = time.Now().Add(-retention)
		} else {
			// Try duration first, then RFC3339
			if d, err := time.ParseDuration(clearBefore); err == nil {
				cutoff = time.Now().Add(-d)
			} else if t, err := time.Parse(time.RFC3339, clearBefore); err == nil {
				cutoff = t
			} else {
				return fmt.Errorf("invalid --before value %q: must be a duration (e.g. 720h) or RFC3339 timestamp", clearBefore)
			}
		}

		store, err := myevents.NewStorage(logr.Discard(), dbPath)
		if err != nil {
			return fmt.Errorf("failed to open events database %q: %w", dbPath, err)
		}
		defer store.Close()

		ctx := context.Background()

		if clearDryRun {
			var count int
			err = store.DB().GetContext(ctx, &count,
				"SELECT COUNT(*) FROM events WHERE ts < ?", float64(cutoff.Unix()))
			if err != nil {
				return fmt.Errorf("failed to count events: %w", err)
			}
			fmt.Printf("Dry run: would delete %d events older than %s\n", count, cutoff.Format(time.RFC3339))
			return nil
		}

		n, err := store.Purge(ctx, cutoff)
		if err != nil {
			return fmt.Errorf("failed to purge events: %w", err)
		}
		fmt.Printf("Deleted %d events older than %s\n", n, cutoff.Format(time.RFC3339))
		return nil
	},
}

func init() {
	clearCmd.Flags().StringVar(&clearBefore, "before", "", "Delete events older than this (duration like 720h, or RFC3339 timestamp; default: retention threshold)")
	clearCmd.Flags().BoolVar(&clearDryRun, "dry-run", false, "Print what would be deleted without actually deleting")
}
