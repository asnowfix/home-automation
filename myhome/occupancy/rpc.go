package occupancy

import (
	"context"
	"myhome"
	"time"

	"github.com/go-logr/logr"
)

// RPCHandler handles occupancy RPC methods
type RPCHandler struct {
	service *Service
	log     logr.Logger
}

// NewRPCHandler creates an occupancy RPC handler
func NewRPCHandler(log logr.Logger, service *Service) *RPCHandler {
	return &RPCHandler{
		service: service,
		log:     log.WithName("occupancy.rpc"),
	}
}

// RegisterHandlers registers occupancy RPC methods
func (h *RPCHandler) RegisterHandlers() {
	myhome.RegisterMethodHandler(myhome.OccupancyGetStatus, h.handleGetStatus)
	h.log.Info("Occupancy RPC handler registered")
}

// handleGetStatus returns the current occupancy status
func (h *RPCHandler) handleGetStatus(ctx context.Context, params any) (any, error) {
	// Get occupancy status using the service's IsOccupied method
	occupied := h.service.IsOccupied(ctx)

	return &myhome.OccupancyStatusResult{
		Occupied: occupied,
	}, nil
}

// GetOccupancyService returns the underlying occupancy service
// This allows external access to the service for RPC integration
func (s *Service) GetOccupancyService() *Service {
	return s
}

// IsOccupied returns whether the home is currently occupied
func (s *Service) IsOccupied(ctx context.Context) bool {
	now := time.Now().UnixNano()
	lastEvent := s.lastEvent.Load()
	lastMobile := s.lastMobileSeen.Load()

	windowNano := s.lastSeenWindow.Nanoseconds()

	hasRecentEvent := (now - lastEvent) <= windowNano
	hasRecentMobile := (now - lastMobile) <= windowNano

	return hasRecentEvent || hasRecentMobile
}
