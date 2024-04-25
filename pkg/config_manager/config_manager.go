package configmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	configmap "github.com/telekom/das-schiff-network-operator/pkg/config_map"
	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
	"github.com/telekom/das-schiff-network-operator/pkg/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultCooldownTime = time.Millisecond * 100
)

type ConfigManager struct {
	client       client.Client
	configs      *configmap.ConfigMap
	cr           *reconciler.ConfigReconciler
	nr           *reconciler.NodeReconciler
	changes      chan bool
	deletedNodes chan []string
	logger       logr.Logger
}

func New(c client.Client, cr *reconciler.ConfigReconciler, nr *reconciler.NodeReconciler, log logr.Logger, changes chan bool, deleteNodes chan []string) *ConfigManager {
	return &ConfigManager{
		client:       c,
		configs:      configmap.New(),
		cr:           cr,
		nr:           nr,
		logger:       log,
		changes:      changes,
		deletedNodes: deleteNodes,
	}
}

func (cm *ConfigManager) WatchDeletedNodes() {
	cm.logger.Info("starting watching for delete nodes...")
	for {
		select {
		case nodes := <-cm.deletedNodes:
			cm.logger.Info("nodes deleted", "nodes", nodes)
			for _, n := range nodes {
				config := cm.configs.Get(n)
				cm.configs.Remove(n)
				config.SetActive(false)
				cancel := config.GetCancelFunc()
				if cancel != nil {
					cancel()
				}
			}
		default:
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cm *ConfigManager) WatchConfigs() error {
	cm.logger.Info("starting watching for changes...")
	for {
		select {
		case <-cm.changes:
			cm.logger.Info("changes occurred")
			err := cm.UpdateConfigs()
			if err != nil {
				return fmt.Errorf("error updating configs: %w", err)
			}
			err = cm.DeployConfigs()
			if err != nil {
				cm.logger.Error(err, "error deploying configs")
				if err := cm.RestoreBackup(); err != nil {
					return fmt.Errorf("error restorigng backup: %w", err)
				}
			}
		default:
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cm *ConfigManager) UpdateConfigs() error {
	cm.logger.Info("UpdateConfigs()")
	currentNodes := cm.nr.GetNodes()
	for name := range currentNodes {
		cm.logger.Info("Creating config for node", "name", name)
		n := currentNodes[name]
		next, err := cm.cr.CreateConfigForNode(name, &n)
		if err != nil {
			return fmt.Errorf("error creating config for the node %s: %w", name, err)
		}
		if cfg := cm.configs.Get(name); cfg != nil {
			cm.logger.Info("Updating", "name", name)
			cfg.UpdateNext(next)
		} else {
			cm.logger.Info("creating", "name", name)
			cfg = nodeconfig.NewEmpty(name)
			cfg.UpdateNext(next)
			cm.configs.Add(name, cfg)
		}
	}
	return nil
}

func (cm *ConfigManager) Deploy(ctx context.Context, configs []*nodeconfig.Config) error {
	cm.logger.Info("Deploy()")
	for _, cfg := range configs {
		cfg.SetDeployed(false)
	}

	for _, cfg := range configs {
		cm.logger.Info("processing", "name", cfg.GetName())
		if cfg.GetActive() {
			cm.logger.Info("is active", "name", cfg.GetName())
			cfgContext, cfgCancel := context.WithCancel(ctx)
			cfg.SetCancelFunc(cfgCancel)
			cm.logger.Info("cancel func set", "name", cfg.GetName())
			if err := cfg.Deploy(cfgContext, cm.client); err != nil {
				return fmt.Errorf("error deploying config %s: %w", cfg.GetName(), err)
			}
		}
	}

	return nil
}

func (cm *ConfigManager) DeployConfigs() error {
	cm.logger.Info("DeployConfigs()")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()
	toDeploy := cm.configs.GetSlice()

	if err := cm.Deploy(ctx, toDeploy); err != nil {
		return fmt.Errorf("error deploying configs: %w", err)
	}

	return nil
}

func (cm *ConfigManager) RestoreBackup() error {
	cm.logger.Info("RestoreBackup()")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()
	slice := cm.configs.GetSlice()
	toDeploy := []*nodeconfig.Config{}
	for _, cfg := range slice {
		if cfg.GetDeployed() {
			cfg.SetBackupAsNext()
			toDeploy = append(toDeploy, cfg)
		}
	}

	if err := cm.Deploy(ctx, toDeploy); err != nil {
		return fmt.Errorf("error deploying configs: %w", err)
	}

	return nil
}
