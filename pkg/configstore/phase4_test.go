package configstore

import "testing"

func TestPhase4Migrations(t *testing.T) {
	s, err := OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Exercise every Phase 4 table with an Upsert/Create + List/Get.
	if err := s.CreateKissInterface(&KissInterface{Name: "tcp0", InterfaceType: "tcp", ListenAddr: "127.0.0.1:8001", Channel: 1, Broadcast: true, Enabled: true}); err != nil {
		t.Fatalf("kiss create: %v", err)
	}
	if ks, err := s.ListKissInterfaces(); err != nil || len(ks) != 1 {
		t.Fatalf("list kiss: %v len=%d", err, len(ks))
	}

	if err := s.UpsertAgwConfig(&AgwConfig{ListenAddr: "0.0.0.0:8000", Callsigns: "N0CALL", Enabled: true}); err != nil {
		t.Fatalf("agw upsert: %v", err)
	}
	if c, err := s.GetAgwConfig(); err != nil || c == nil || c.ListenAddr != "0.0.0.0:8000" {
		t.Fatalf("agw get: %v %+v", err, c)
	}

	if err := s.UpsertTxTiming(&TxTiming{Channel: 1, TxDelayMs: 250, TxTailMs: 100, SlotMs: 100, Persist: 63}); err != nil {
		t.Fatalf("tx timing upsert: %v", err)
	}
	if err := s.UpsertTxTiming(&TxTiming{Channel: 1, TxDelayMs: 400, TxTailMs: 100, SlotMs: 100, Persist: 63}); err != nil {
		t.Fatalf("tx timing second upsert: %v", err)
	}
	if ts, err := s.ListTxTimings(); err != nil || len(ts) != 1 || ts[0].TxDelayMs != 400 {
		t.Fatalf("tx list: %v %+v", err, ts)
	}

	if err := s.UpsertDigipeaterConfig(&DigipeaterConfig{Enabled: true, DedupeWindowSeconds: 30, MyCall: "N0CAL"}); err != nil {
		t.Fatalf("digi cfg: %v", err)
	}
	if err := s.CreateDigipeaterRule(&DigipeaterRule{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat", Enabled: true}); err != nil {
		t.Fatalf("digi rule: %v", err)
	}
	if rs, err := s.ListDigipeaterRulesForChannel(1); err != nil || len(rs) != 1 {
		t.Fatalf("digi rule list: %v len=%d", err, len(rs))
	}

	if err := s.UpsertIGateConfig(&IGateConfig{Enabled: true, Server: "rotate.aprs2.net", Port: 14580, Callsign: "N0CALL", Passcode: "-1"}); err != nil {
		t.Fatalf("igate cfg: %v", err)
	}
	if err := s.CreateIGateRfFilter(&IGateRfFilter{Channel: 1, Type: "callsign", Pattern: "KK6*", Action: "allow", Priority: 100, Enabled: true}); err != nil {
		t.Fatalf("igate filter: %v", err)
	}
	if fs, err := s.ListIGateRfFiltersForChannel(1); err != nil || len(fs) != 1 {
		t.Fatalf("igate filter list: %v len=%d", err, len(fs))
	}

	if err := s.CreateBeacon(&Beacon{Type: "position", Channel: 1, Callsign: "N0CAL", Path: "WIDE1-1", Latitude: 40, Longitude: -105, SymbolTable: "/", Symbol: ">", EverySeconds: 1800, Enabled: true}); err != nil {
		t.Fatalf("beacon create: %v", err)
	}
	if bs, err := s.ListBeacons(); err != nil || len(bs) != 1 {
		t.Fatalf("beacon list: %v len=%d", err, len(bs))
	}

	if _, err := s.ListPacketFilters(); err != nil {
		t.Fatalf("packet filter list: %v", err)
	}
}
