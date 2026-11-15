package modembridge

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestFX25IndependentLengthVectors(t *testing.T) {
	data, err := os.ReadFile("../../graywolf-modem/testdata/fx25/direwolf.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Frame string `json:"frame"`
		} `json:"wire_vectors"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, v := range fixture.Vectors {
		frame, _ := hex.DecodeString(v.Frame)
		if err := validateFX25Frame(frame); err != nil {
			t.Fatal(err)
		}
	}
	for _, size := range []int{0, 14, 236, 1000} {
		if validateFX25Frame(make([]byte, size)) == nil {
			t.Fatalf("accepted %d", size)
		}
	}
	stuffed := make([]byte, 235)
	for i := range stuffed {
		stuffed[i] = 255
	}
	if validateFX25Frame(stuffed) == nil {
		t.Fatal("ignored stuffing")
	}
	if err := validateFX25Frame(make([]byte, 235)); err != nil {
		t.Fatal(err)
	}
}
