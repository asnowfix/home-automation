package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/asnowfix/home-automation/hlog"
	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/ctl/options"
	"github.com/asnowfix/home-automation/pkg/devices"
	"github.com/asnowfix/home-automation/pkg/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

var Cmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the MCP stdio server (for AI tool use)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := server.NewMCPServer("shelly", "1.0.0")

		srv.AddTool(
			mcpgo.NewTool("shelly_list",
				mcpgo.WithDescription("List all Shelly devices known to the myhome daemon (id, name, host, mac)."),
				mcpgo.WithString("filter",
					mcpgo.Description(`Optional name substring filter, e.g. "pool". Defaults to "*" (all devices).`)),
			),
			handleList,
		)
		srv.AddTool(
			mcpgo.NewTool("shelly_call",
				mcpgo.WithDescription("Call a Shelly Gen2+ device RPC method over MQTT and return the JSON result."),
				mcpgo.WithString("device_id",
					mcpgo.Required(),
					mcpgo.Description("Shelly device ID or name pattern, e.g. shellypm-abc123")),
				mcpgo.WithString("method",
					mcpgo.Required(),
					mcpgo.Description("RPC method name, e.g. Shelly.GetStatus, Switch.Set, KVS.Get")),
				mcpgo.WithString("params",
					mcpgo.Description(`JSON object of method parameters, e.g. {"id":0,"on":true}. Defaults to {}.`)),
			),
			handleCall,
		)

		return server.ServeStdio(srv)
	},
}

func handleList(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	filter := req.GetString("filter", "*")
	if filter == "" {
		filter = "*"
	}

	devs, err := myhome.TheClient.LookupDevices(ctx, filter)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	out, _ := json.MarshalIndent(devs, "", "  ")
	return mcpgo.NewToolResultText(string(out)), nil
}

func handleCall(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	deviceID, err := req.RequireString("device_id")
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	method, err := req.RequireString("method")
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}
	paramsStr := req.GetString("params", "{}")

	var params any
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("invalid params JSON: %v", err)), nil
	}

	result, err := myhome.Foreach(ctx, hlog.Logger, deviceID, options.Via,
		func(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, _ []string) (any, error) {
			sd, ok := device.(*shelly.Device)
			if !ok {
				return nil, fmt.Errorf("device is not a Shelly: %v", reflect.TypeOf(device))
			}
			return sd.CallE(ctx, via, method, params)
		}, nil)
	if err != nil {
		return mcpgo.NewToolResultError(err.Error()), nil
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return mcpgo.NewToolResultText(string(out)), nil
}
