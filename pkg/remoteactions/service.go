package remoteactions

import (
	"errors"
	"log/slog"

	"gorm.io/gorm"
)

// Service is the composition root for the outbound-Actions package.
// It owns the two stores and exposes them to REST handlers via Creds()
// and Macros(). One instance per graywolf process.
//
// Failure to construct (currently only "DB == nil") is non-fatal at
// the wiring layer; the REST handlers fall back to 503 when the
// service is missing.
type Service struct {
	db     *gorm.DB
	creds  *CredStore
	macros *MacroStore
	logger *slog.Logger
}

// ServiceConfig wires the service to its dependencies.
type ServiceConfig struct {
	DB     *gorm.DB     // required
	Logger *slog.Logger // optional; nil -> slog.Default()
}

// NewService constructs the service. Returns an error when DB is nil
// so the wiring layer can log and skip rather than panic.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.DB == nil {
		return nil, errors.New("remoteactions: ServiceConfig.DB required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		db:     cfg.DB,
		creds:  NewCredStore(cfg.DB),
		macros: NewMacroStore(cfg.DB),
		logger: logger,
	}, nil
}

func (s *Service) Creds() *CredStore   { return s.creds }
func (s *Service) Macros() *MacroStore { return s.macros }
