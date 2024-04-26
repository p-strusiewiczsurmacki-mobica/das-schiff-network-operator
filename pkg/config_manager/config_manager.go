package configmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	configmap "github.com/telekom/das-schiff-network-operator/pkg/config_map"
	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
	"github.com/telekom/das-schiff-network-operator/pkg/reconciler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultCooldownTime = time.Millisecond * 100
	processName         = "configurator"
)

type ConfigManager struct {
	client       client.Client
	configs      *configmap.ConfigMap
	cr           *reconciler.ConfigReconciler
	nr           *reconciler.NodeReconciler
	changes      chan bool
	deletedNodes chan []string
	logger       logr.Logger
	timeout      time.Duration
}

func New(c client.Client, cr *reconciler.ConfigReconciler, nr *reconciler.NodeReconciler, log logr.Logger, timeout time.Duration, changes chan bool, deleteNodes chan []string) *ConfigManager {
	return &ConfigManager{
		client:       c,
		configs:      configmap.New(),
		cr:           cr,
		nr:           nr,
		logger:       log,
		changes:      changes,
		deletedNodes: deleteNodes,
		timeout:      timeout,
	}
}

func (cm *ConfigManager) WatchDeletedNodes(ctx context.Context, errCh chan error) error {
	cm.logger.Info("starting watching for delete nodes...")
	for {
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.Canceled) {
				errCh <- fmt.Errorf("error watching deleted nodes: %w", ctx.Err())
			}
			errCh <- nil
		case nodes := <-cm.deletedNodes:
			cm.logger.Info("nodes deleted", "nodes", nodes)
			for _, n := range nodes {
				config := cm.configs.Get(n)
				cm.configs.Remove(n)
				config.SetActive(false)
				cm.logger.Info("Get cancel func")
				cancel := config.GetCancelFunc()
				if cancel != nil {
					cancel()
					cm.logger.Info("cancel function called")
				}
				err := config.Prune(ctx, cm.client)
				if err != nil {
					cm.logger.Error(err, "error deleting node configuration objects")
				}
			}
		default:
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cm *ConfigManager) WatchConfigs(ctx context.Context, errCh chan error) {
	cm.logger.Info("starting watching for changes...")
	for {
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.Canceled) {
				errCh <- fmt.Errorf("error watching configs: %w", ctx.Err())
			}
			errCh <- nil
		case <-cm.changes:
			cm.logger.Info("changes occurred")
			err := cm.UpdateConfigs()
			if err != nil {
				errCh <- fmt.Errorf("error updating configs: %w", err)
			}
			err = cm.DeployConfigs(ctx)
			if err != nil {
				cm.logger.Error(err, "error deploying configs")
				if err := cm.RestoreBackup(ctx); err != nil {
					errCh <- fmt.Errorf("error restorigng backup: %w", err)
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

	if err := cm.setProcessStatus(ctx, nodeconfig.StatusProvisioning); err != nil {
		return fmt.Errorf("error setting process status: %w", err)
	}

	for _, cfg := range configs {
		cm.logger.Info("processing", "name", cfg.GetName())
		if cfg.GetActive() {
			cm.logger.Info("is active", "name", cfg.GetName())
			cfgContext, cfgCancel := context.WithTimeout(ctx, cm.timeout)
			cfg.SetCancelFunc(cfgCancel)
			cm.logger.Info("cancel func set", "name", cfg.GetName())
			if err := cfg.Deploy(cfgContext, cm.client, cm.logger); err != nil {
				if err := cfg.CrateInvalid(ctx, cm.client); err != nil {
					return fmt.Errorf("error creating invalid config object: %w", err)
				}
				return fmt.Errorf("error deploying config %s: %w", cfg.GetName(), err)
			}
		}
	}

	if err := cm.setProcessStatus(ctx, nodeconfig.StatusProvisioned); err != nil {
		return fmt.Errorf("error setting process status: %w", err)
	}

	return nil
}

func (cm *ConfigManager) setProcessStatus(ctx context.Context, status string) error {
	process, err := cm.getProcess(ctx)
	if err != nil && apierrors.IsNotFound(err) {
		process.Spec.State = status
		if err := cm.client.Create(ctx, process); err != nil {
			return fmt.Errorf("error creating process object: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error getting proces object: %w", err)
	}
	process.Spec.State = status
	if err := cm.client.Update(ctx, process); err != nil {
		return fmt.Errorf("error updating process object: %w", err)
	}
	return nil
}

func (cm *ConfigManager) DeployConfigs(ctx context.Context) error {
	cm.logger.Info("DeployConfigs()")
	toDeploy := cm.configs.GetSlice()

	if err := cm.Deploy(ctx, toDeploy); err != nil {
		return fmt.Errorf("error deploying configs: %w", err)
	}

	return nil
}

func (cm *ConfigManager) RestoreBackup(ctx context.Context) error {
	cm.logger.Info("RestoreBackup()")
	slice := cm.configs.GetSlice()
	toDeploy := []*nodeconfig.Config{}
	for _, cfg := range slice {
		if cfg.GetDeployed() {
			if backupAvailable := cfg.SetBackupAsNext(); backupAvailable {
				toDeploy = append(toDeploy, cfg)
			}
		}
	}

	if err := cm.Deploy(ctx, toDeploy); err != nil {
		return fmt.Errorf("error deploying configs: %w", err)
	}

	return nil
}

func (cm *ConfigManager) getProcess(ctx context.Context) (*v1alpha1.NodeConfigProcess, error) {
	process := &v1alpha1.NodeConfigProcess{
		ObjectMeta: v1.ObjectMeta{
			Name: processName,
		},
	}
	if err := cm.client.Get(ctx, client.ObjectKeyFromObject(process), process); err != nil {
		return process, fmt.Errorf("error getting process object: %w", err)
	}
	return process, nil
}

// DirtyStartup will load all previous depoyed NodeConfigs into current leader
func (cm *ConfigManager) DirtyStartup(ctx context.Context) error {
	process, err := cm.getProcess(ctx)
	if err != nil {
		// process object does not exists - there was no operator running on this cluster before
		if apierrors.IsNotFound(err) {
			return nil
		} else {
			return fmt.Errorf("error geting process object: %w", err)
		}
	}

	cm.logger.Info("restoring previous leader work")

	// get all known backup data and deploy it
	cm.logger.Info("listing all worker nodes")
	nodes, err := reconciler.ListNodes(ctx, cm.client)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	cm.logger.Info("listing all nodeconfigs")
	knownConfigs := &v1alpha1.NodeConfigList{}
	if err := cm.client.List(ctx, knownConfigs); err != nil {
		return fmt.Errorf("error listing NodeConfigs: %w", err)
	}

	cm.logger.Info("processing items")
	for name := range nodes {
		var current *v1alpha1.NodeConfig
		var backup *v1alpha1.NodeConfig
		var invalid *v1alpha1.NodeConfig
		for j := range knownConfigs.Items {
			if knownConfigs.Items[j].Name == name {
				current = &knownConfigs.Items[j]
			}
			if knownConfigs.Items[j].Name == name+nodeconfig.InvalidSuffix {
				invalid = &knownConfigs.Items[j]
			}
			if knownConfigs.Items[j].Name == name+nodeconfig.BackupSuffix {
				backup = &knownConfigs.Items[j]
			}
		}
		cfg := nodeconfig.New(name, current, backup, invalid)
		cm.logger.Info("adding config for node", "name", name)
		cm.configs.Add(name, cfg)
	}

	if process.Spec.State == nodeconfig.StatusProvisioning {
		// prevouos leader left cluster in provisioning state - cleanup
		cm.logger.Info("cleanup needed - restoring backup")
		if err := cm.RestoreBackup(ctx); err != nil {
			return fmt.Errorf("error restoring backup: %w", err)
		}
	}
	return nil
}
