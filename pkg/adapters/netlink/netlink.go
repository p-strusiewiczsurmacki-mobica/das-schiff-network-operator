package adapters

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
	"github.com/telekom/das-schiff-network-operator/pkg/frr"
	"github.com/telekom/das-schiff-network-operator/pkg/healthcheck"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
)

type Common struct {
	NetlinkManager *nl.Manager
	Config         *config.Config
	FrrManager     *frr.Manager
	AnycastTracker *anycast.Tracker
	DirtyFRRConfig bool
	HealthChecker  *healthcheck.HealthChecker
	Logger         logr.Logger
}

func New(anycastTracker *anycast.Tracker, logger logr.Logger) (*Common, error) {
	reconciler := &Common{
		NetlinkManager: nl.NewManager(&nl.Toolkit{}),
		FrrManager:     frr.NewFRRManager(),
		AnycastTracker: anycastTracker,
		Logger:         logger,
	}
	if val := os.Getenv("FRR_CONFIG_FILE"); val != "" {
		reconciler.FrrManager.ConfigPath = val
	}
	if err := reconciler.FrrManager.Init(); err != nil {
		return nil, fmt.Errorf("error trying to init FRR Manager: %w", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}
	reconciler.Config = cfg

	nc, err := healthcheck.LoadConfig(healthcheck.NetHealthcheckFile)
	if err != nil {
		return nil, fmt.Errorf("error loading networking healthcheck config: %w", err)
	}

	reconciler.HealthChecker, err = healthcheck.NewHealthChecker(nil,
		healthcheck.NewDefaultHealthcheckToolkit(reconciler.FrrManager, nil),
		nc)
	if err != nil {
		return nil, fmt.Errorf("error creating netwokring healthchecker: %w", err)
	}

	return reconciler, nil
}

func (c *Common) CheckHealth() error {
	if _, err := c.HealthChecker.IsFRRActive(); err != nil {
		return fmt.Errorf("error checking FRR status: %w", err)
	}
	if err := c.HealthChecker.CheckInterfaces(); err != nil {
		return fmt.Errorf("error checking network interfaces: %w", err)
	}
	return nil
}

func (c *Common) GetConfig() *config.Config {
	return c.Config
}
