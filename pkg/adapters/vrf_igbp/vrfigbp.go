package adapters

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
	"github.com/telekom/das-schiff-network-operator/pkg/frr"
	"github.com/telekom/das-schiff-network-operator/pkg/healthcheck"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
)

type VrfIgbp struct {
	netlinkManager *nl.Manager
	config         *config.Config
	frrManager     *frr.Manager
	anycastTracker *anycast.Tracker
	dirtyFRRConfig bool
	healthChecker  *healthcheck.HealthChecker
	logger         logr.Logger
}

func New(anycastTracker *anycast.Tracker, logger logr.Logger) (agent.Adapter, error) {
	reconciler := &VrfIgbp{
		netlinkManager: nl.NewManager(&nl.Toolkit{}),
		frrManager:     frr.NewFRRManager(),
		anycastTracker: anycastTracker,
		logger:         logger,
	}

	if val := os.Getenv("FRR_CONFIG_FILE"); val != "" {
		reconciler.frrManager.ConfigPath = val
	}
	if err := reconciler.frrManager.Init(); err != nil {
		return nil, fmt.Errorf("error trying to init FRR Manager: %w", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}
	reconciler.config = cfg

	nc, err := healthcheck.LoadConfig(healthcheck.NetHealthcheckFile)
	if err != nil {
		return nil, fmt.Errorf("error loading networking healthcheck config: %w", err)
	}

	reconciler.healthChecker, err = healthcheck.NewHealthChecker(nil,
		healthcheck.NewDefaultHealthcheckToolkit(reconciler.frrManager, nil),
		nc)
	if err != nil {
		return nil, fmt.Errorf("error creating netwokring healthchecker: %w", err)
	}

	return reconciler, nil
}

func (r *VrfIgbp) CheckHealth() error {
	if _, err := r.healthChecker.IsFRRActive(); err != nil {
		return fmt.Errorf("error checking FRR status: %w", err)
	}
	if err := r.healthChecker.CheckInterfaces(); err != nil {
		return fmt.Errorf("error checking network interfaces: %w", err)
	}
	return nil
}

func (r *VrfIgbp) GetConfig() *config.Config {
	return r.config
}
