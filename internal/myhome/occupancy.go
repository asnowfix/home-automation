package myhome

// Occupancy RPC types

// OccupancyGetStatusParams represents parameters for occupancy.getstatus
type OccupancyGetStatusParams struct {
	// No parameters needed - returns overall occupancy status
}

// OccupancyStatusResult represents the result of occupancy.getstatus
type OccupancyStatusResult struct {
	Occupied bool `json:"occupied"`
}
