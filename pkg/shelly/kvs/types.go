// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/KVS/
package kvs

type Key struct {
	Key string `json:"key"` // The key to be looked-up / added / updated. (Required)
}

type KeyValue struct {
	Key
	Value
}

type Value struct {
	Value string `json:"value"` // any JSON value to be added / updated.
	Status
}

type Status struct {
	Etag *string `json:"etag,omitempty"` // Generated hash uniquely identifying the key-value pair. Optional
	Rev  *uint32 `json:"rev,omitempty"`  // Revision number of the key-value pair (after update). Optional
}

type KeyValuesMatching struct {
	Match string `json:"match,omitempty"` // Pattern against which keys are matched. default is * which matches all.
}

type KeyItems struct {
	Keys     map[string]Status `json:"keys"` // Whose keys are the keys which matched against the requested pattern and the only property of the corresponding etag
	Revision uint32            `json:"rev"`  // Current revision of the store
}

type KeyValueItems struct {
	Items map[string]Value `json:"items"` // The key-value pairs to be added / updated.
}
