package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRadioBaudUpgradeDefaultsToPreserve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-kiss.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec(`INSERT INTO kiss_interfaces (name,interface_type,device,baud_rate,init_profile,mode,created_at,updated_at)
	 VALUES ('existing','serial','/dev/ttyTEST',19200,'kenwood-tm-d710','tnc',datetime('now'),datetime('now'))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec("ALTER TABLE kiss_interfaces DROP COLUMN radio_baud_rate").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.ListKissInterfaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RadioBaudRate != 0 || rows[0].BaudRate != 19200 || rows[0].InitProfile != "kenwood-tm-d710" || rows[0].Device != "/dev/ttyTEST" {
		t.Fatalf("upgrade changed interface: %+v", rows)
	}
	invalid := rows[0]
	invalid.RadioBaudRate = 4800
	if err := s.UpdateKissInterface(context.Background(), &invalid); err == nil {
		t.Fatal("store accepted unsupported RF rate")
	}
}
