package submit

import (
	"encoding/json"
	"fmt"
)

// BuildUpdate computes a shallow diff between two flare payloads.
// Output: {"added":{...}, "changed":{key:[old,new]}, "removed":{...}}
func BuildUpdate(prev, fresh []byte) ([]byte, error) {
	var (
		oldMap = map[string]any{}
		newMap = map[string]any{}
	)
	if err := json.Unmarshal(prev, &oldMap); err != nil {
		return nil, fmt.Errorf("decode prev: %w", err)
	}
	if err := json.Unmarshal(fresh, &newMap); err != nil {
		return nil, fmt.Errorf("decode fresh: %w", err)
	}
	added := map[string]any{}
	changed := map[string]any{}
	removed := map[string]any{}
	for k, nv := range newMap {
		if ov, ok := oldMap[k]; !ok {
			added[k] = nv
		} else if !jsonEqual(ov, nv) {
			changed[k] = []any{ov, nv}
		}
	}
	for k, ov := range oldMap {
		if _, ok := newMap[k]; !ok {
			removed[k] = ov
		}
	}
	return json.Marshal(struct {
		Added   map[string]any `json:"added"`
		Changed map[string]any `json:"changed"`
		Removed map[string]any `json:"removed"`
	}{added, changed, removed})
}

func jsonEqual(a, b any) bool {
	ba, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ba) == string(bb)
}
