package configstore

import "testing"

func TestFX25Scope(t *testing.T) {
	good := Channel{InputDeviceID: U32Ptr(1), ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200, FX25Encode: true}
	if err := good.ValidateFX25(); err != nil {
		t.Fatal(err)
	}
	cases := []Channel{good, good, good, good, good, good}
	cases[0].BitRate = 300
	cases[1].ModemType = "9600"
	cases[2].MarkFreq = 1600
	cases[3].SpaceFreq = 1800
	cases[4].IL2PEncode = true
	cases[5].InputDeviceID = nil
	for _, ch := range cases {
		if ch.ValidateFX25() == nil {
			t.Fatalf("accepted %+v", ch)
		}
		ch.FX25Encode = false
		if err := ch.ValidateFX25(); err != nil {
			t.Fatal(err)
		}
	}
}
