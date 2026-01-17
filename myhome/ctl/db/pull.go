package db

import (
	"encoding/json"
	"fmt"
	"io"
	"myhome"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var pullFlags struct {
	Pattern string
	DryRun  bool
}

var PullCmd = &cobra.Command{
	Use:   "pull <remote-url>",
	Short: "Pull devices from a remote myhome server",
	Long: `Pull devices from a remote myhome server's HTTP API.

The remote server must have the HTTP proxy enabled. This command fetches
the device list from the remote server and imports them into the local database.

Examples:
  # Pull all devices from remote server
  myhome ctl db pull http://myhome.local:8080

  # Pull with a specific pattern filter
  myhome ctl db pull http://192.168.1.100:8080 --pattern "shellyblu*"

  # Dry run - show what would be imported without making changes
  myhome ctl db pull http://myhome.local:8080 --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteURL := args[0]

		// Build the URL for the devices endpoint
		url := remoteURL + "/rpc"

		// Create RPC request
		pattern := pullFlags.Pattern
		if pattern == "" {
			pattern = "*"
		}

		rpcRequest := struct {
			Method string `json:"method"`
			Params string `json:"params"`
		}{
			Method: "device.match",
			Params: pattern,
		}

		reqBody, err := json.Marshal(rpcRequest)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		// Make HTTP request
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Post(url, "application/json",
			io.NopCloser(
				&jsonReader{data: reqBody},
			))
		if err != nil {
			return fmt.Errorf("failed to connect to remote server: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("remote server returned error %d: %s", resp.StatusCode, string(body))
		}

		// Parse response
		var rpcResponse struct {
			Result []myhome.Device `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		err = json.NewDecoder(resp.Body).Decode(&rpcResponse)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if rpcResponse.Error != nil {
			return fmt.Errorf("remote RPC error: %s (code %d)", rpcResponse.Error.Message, rpcResponse.Error.Code)
		}

		devices := rpcResponse.Result
		if len(devices) == 0 {
			fmt.Fprintln(os.Stderr, "No devices found on remote server matching pattern:", pattern)
			return nil
		}

		fmt.Fprintf(os.Stderr, "Found %d devices on remote server\n", len(devices))

		if pullFlags.DryRun {
			fmt.Fprintln(os.Stderr, "\n[DRY RUN] Would import the following devices:")
			for _, device := range devices {
				name := device.Name()
				if name == "" {
					name = "(no name)"
				}
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", device.Id(), name)
			}
			return nil
		}

		// Import each device
		imported := 0
		for _, device := range devices {
			_, err := myhome.TheClient.CallE(cmd.Context(), myhome.DeviceUpdate, &device)
			if err != nil {
				fmt.Fprintf(os.Stderr, "⚠ Failed to import %s: %v\n", device.Id(), err)
				continue
			}
			imported++
			name := device.Name()
			if name == "" {
				name = "(no name)"
			}
			fmt.Fprintf(os.Stderr, "✓ Imported %s (%s)\n", name, device.Id())
		}

		fmt.Fprintf(os.Stderr, "\n✓ Pulled %d/%d devices from %s\n", imported, len(devices), remoteURL)
		return nil
	},
}

// jsonReader wraps a byte slice to implement io.Reader
type jsonReader struct {
	data []byte
	pos  int
}

func (r *jsonReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func init() {
	PullCmd.Flags().StringVar(&pullFlags.Pattern, "pattern", "", "Filter devices by pattern (e.g., 'shellyblu*')")
	PullCmd.Flags().BoolVar(&pullFlags.DryRun, "dry-run", false, "Show what would be imported without making changes")
}
