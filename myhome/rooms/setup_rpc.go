package rooms

import (
	"context"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// RoomSetupHandler is the function signature for the room.setup RPC backend.
// The daemon provides this function so the rooms package doesn't need to import devices.
type RoomSetupHandler func(ctx context.Context, roomID string) (*myhome.RoomSetupResult, error)

// RegisterSetupHandler registers the room.setup RPC method with the given backend.
func RegisterSetupHandler(handler RoomSetupHandler) {
	myhome.RegisterMethodHandler(myhome.RoomSetup, func(ctx context.Context, params any) (any, error) {
		p := params.(*myhome.RoomSetupParams)
		if p.RoomID != "" {
			return handler(ctx, p.RoomID)
		}
		// Empty room_id = setup all rooms; return a placeholder result.
		// The daemon handles this via SetupAllRooms directly.
		return &myhome.RoomSetupResult{}, nil
	})
}
