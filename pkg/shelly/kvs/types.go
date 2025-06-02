package kvs

import (
	"encoding/json"
	"fmt"
)

// <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS/>

type KeyValue struct {
	Key   string `json:"key"`   // The key to be looked-up / added / updated. (Required)
	Value string `json:"value"` // any JSON value to be added / updated.
	Status
}

type Value struct {
	Value string `json:"value"` // any JSON value to be added / updated.
	Status
}

type Status struct {
	Etag *string `json:"etag,omitempty"` // Generated hash uniquely identifying the key-value pair. Optional
	Rev  *uint32 `json:"rev,omitempty"`  // Revision number of the key-value pair (after update). Optional
}

type ListResponse struct {
	Keys     map[string]Status `json:"keys"` // Whose keys are the keys which matched against the requested pattern and the only property of the corresponding etag
	Revision uint32            `json:"rev"`  // Current revision of the store
}

type GetRequest struct {
	Key string `json:"key"`
}

type GetResponse struct {
	Value string `json:"value"`
	Status
}

type GetManyRequest struct {
	Match  string `json:"match"`
	Offset uint32 `json:"offset,omitempty"`
}

// FlexibleMap can be either a map[string]string or []KeyValue
type FlexibleMap map[string]string

// UnmarshalJSON implements the json.Unmarshaler interface
func (fm *FlexibleMap) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as a map
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		*fm = m
		return nil
	}

	// If that fails, try to unmarshal as an array of KeyValue
	var kvs []KeyValue
	if err := json.Unmarshal(data, &kvs); err == nil {
		result := make(map[string]string, len(kvs))
		for _, kv := range kvs {
			result[kv.Key] = kv.Value
		}
		*fm = result
		return nil
	}

	return fmt.Errorf("could not unmarshal into either map[string]string or []KeyValue")
}

type GetManyResponse struct {
	Items  FlexibleMap `json:"items"`
	Offset uint32      `json:"offset,omitempty"`
	Total  uint32      `json:"total,omitempty"`
}
