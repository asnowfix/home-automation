package occupancy

import (
	"myhome"

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
func (h *RPCHandler) handleGetStatus(params any) (any, error) {
	// Get occupancy status using the service's default window
	occupied, _ := h.service.isOccupied(h.service.window)

	return &myhome.OccupancyStatusResult{
		Occupied: occupied,
	}, nil
}

// GetOccupancyService returns the underlying occupancy service
// This allows external access to the service for RPC integration
func (s *Service) GetOccupancyService() *Service {
	return s
}

// IsOccupied is a public wrapper around isOccupied for RPC handler
func (s *Service) IsOccupied() bool {
	occupied, _ := s.isOccupied(s.window)
	return occupied
}
