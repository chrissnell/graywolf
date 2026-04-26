package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUserJSONOmitsEmpty(t *testing.T) {
	u := User{}
	got, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{}` {
		t.Fatalf("got %s, want {} (every User field is omitempty)", got)
	}
}

func TestUserJSONFull(t *testing.T) {
	u := User{
		Email:          "user@example.com",
		Notes:          "PTT not keying",
		RadioModel:     "FT-991A",
		AudioInterface: "Digirig",
	}
	got, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"email":"user@example.com"`,
		`"notes":"PTT not keying"`,
		`"radio_model":"FT-991A"`,
		`"audio_interface":"Digirig"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("got %s, missing %s", got, want)
		}
	}
}

func TestMetaCarriesBothBinaryVersions(t *testing.T) {
	m := Meta{
		SchemaVersion:        1,
		GraywolfVersion:      "0.43.2",
		GraywolfCommit:       "abc1234",
		GraywolfModemVersion: "0.11.4",
		GraywolfModemCommit:  "def5678",
		HostnameHash:         "1a2b3c4d",
		SubmittedAt:          time.Date(2026, 4, 25, 18, 30, 0, 0, time.UTC),
	}
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"schema_version":1`,
		`"graywolf_version":"0.43.2"`,
		`"graywolf_commit":"abc1234"`,
		`"graywolf_modem_version":"0.11.4"`,
		`"graywolf_modem_commit":"def5678"`,
		`"hostname_hash":"1a2b3c4d"`,
		`"submitted_at":"2026-04-25T18:30:00Z"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("got %s, missing %s", got, want)
		}
	}
}
