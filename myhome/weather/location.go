package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type ipAPIResponse struct {
	Status string  `json:"status"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
}

// IPGeoLocation fetches coordinates from ip-api.com (free, no authentication).
func IPGeoLocation(ctx context.Context) (float64, float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://ip-api.com/json?fields=status,lat,lon", nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var geo ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return 0, 0, err
	}
	if geo.Status != "success" {
		return 0, 0, fmt.Errorf("ip-api.com: status %q", geo.Status)
	}
	if geo.Lat == 0 && geo.Lon == 0 {
		return 0, 0, fmt.Errorf("ip-api.com: returned zero coordinates")
	}
	return geo.Lat, geo.Lon, nil
}
