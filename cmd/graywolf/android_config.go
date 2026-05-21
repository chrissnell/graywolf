// android_config.go is intentionally not build-tagged so its logic
// can be unit-tested on any host. main_android.go is the entry that
// actually consumes configFromEnv on Android builds; on desktop the
// function is dead code (the desktop main.go uses flags).
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/app"
)

// configFromEnv builds an app.Config from the Service-injected env
// vars. Missing required vars are fatal; the Service supervisor will
// restart but will keep failing -- visible in logcat.
//
// GRAYWOLF_LOG_DB is not consumed yet: the desktop logbuffer wires
// itself via configstore at runtime; an Android-specific override
// would require a new app.Config field. Defer to a later phase.
func configFromEnv() (app.Config, error) {
	must := func(k string) (string, error) {
		v := os.Getenv(k)
		if v == "" {
			return "", fmt.Errorf("required env %s is empty", k)
		}
		return v, nil
	}

	dbPath, err := must("GRAYWOLF_DB")
	if err != nil {
		return app.Config{}, err
	}
	historyDB, err := must("GRAYWOLF_HISTORY_DB")
	if err != nil {
		return app.Config{}, err
	}
	tileCache, err := must("GRAYWOLF_TILE_CACHE")
	if err != nil {
		return app.Config{}, err
	}
	modemSock, err := must("GRAYWOLF_MODEM_SOCKET")
	if err != nil {
		return app.Config{}, err
	}
	listen, err := must("GRAYWOLF_LISTEN")
	if err != nil {
		return app.Config{}, err
	}
	token, err := must("GRAYWOLF_LISTEN_TOKEN")
	if err != nil {
		return app.Config{}, err
	}

	cfg := app.Config{
		DBPath:          dbPath,
		HistoryDBPath:   historyDB,
		TileCacheDir:    tileCache,
		ModemSocketPath: modemSock,
		HTTPAddr:        listen,
		BearerToken:     token,
		ShutdownTimeout: 10 * time.Second,
	}
	return cfg, nil
}
