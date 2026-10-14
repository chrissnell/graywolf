package webapi

import (
	"encoding/json"
	"github.com/chrissnell/graywolf/pkg/igate"
	"testing"
)

func TestStatusRFToISMasterJSON(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		wire, err := json.Marshal(newStatusIgateDTO(igate.Status{RFToISEnabled: enabled}))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(wire, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["rf_to_is_enabled"] != enabled {
			t.Fatalf("missing or wrong switch: %s", wire)
		}
	}
}
