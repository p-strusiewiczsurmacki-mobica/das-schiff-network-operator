package configmanager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	configmap "github.com/telekom/das-schiff-network-operator/pkg/config_map"
	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
	"github.com/telekom/das-schiff-network-operator/pkg/reconciler"
	"golang.org/x/sync/semaphore"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	sem          semaphore.Weighted
}

func New(c client.Client, cr *reconciler.ConfigReconciler, nr *reconciler.NodeReconciler, log logr.Logger,
	timeout time.Duration, limit int64, changes chan bool, deleteNodes chan []string) *ConfigManager {
	// disable gradual rolllout if limit is < 1
	if limit < 1 {
		limit = math.MaxInt64
	}
	return &ConfigManager{
		client:       c,
		configs:      configmap.New(),
		cr:           cr,
		nr:           nr,
		logger:       log,
		changes:      changes,
		deletedNodes: deleteNodes,
		timeout:      timeout,
		sem:          *semaphore.NewWeighted(limit),
	}
}

func (cm *ConfigManager) WatchDeletedNodes(ctx context.Context, errCh chan error) {
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
				cancel := config.GetCancelFunc()
				if cancel != nil {
					cancel()
				}
				err := config.Prune(ctx, cm.client)
				if err != nil {
					cm.logger.Error(err, "error deleting node configuration objects")
				}
				cm.logger.Info("removed node data", "name", n)
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
			cm.logger.Info("got notification about changes")
			err := cm.UpdateConfigs()
			if err != nil {
				errCh <- fmt.Errorf("error updating configs: %w", err)
			}
			err = cm.DeployConfigs(ctx)
			if err != nil {
				if err := cm.RestoreBackup(ctx); err != nil {
					cm.logger.Error(err, "error restoring backup")
				}
			}
		default:
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cm *ConfigManager) UpdateConfigs() error {
	cm.logger.Info("updating configs...")
	currentNodes := cm.nr.GetNodes()
	for name := range currentNodes {
		n := currentNodes[name]
		next, err := cm.cr.CreateConfigForNode(name, n)
		if err != nil {
			return fmt.Errorf("error creating config for the node %s: %w", name, err)
		}
		if cfg := cm.configs.Get(name); cfg != nil {
			cfg.UpdateNext(next)
		} else {
			cfg = nodeconfig.NewEmpty(name)
			cfg.UpdateNext(next)
			cm.configs.Add(name, cfg)
		}
		cm.logger.Info("set up config for node", "name", name)
	}
	return nil
}

func (cm *ConfigManager) Deploy(ctx context.Context, configs []*nodeconfig.Config) error {
	for _, cfg := range configs {
		cfg.SetDeployed(false)
	}

	if err := cm.validateConfigs(configs); err != nil {
		return fmt.Errorf("error validating configs: %w", err)
	}

	if err := cm.setProcessStatus(ctx, nodeconfig.StatusProvisioning); err != nil {
		return fmt.Errorf("error setting process status: %w", err)
	}

	deploymentCtx, deploymentCancel := context.WithCancel(ctx)
	defer deploymentCancel()

	wg := &sync.WaitGroup{}
	errCh := make(chan error, len(configs))
	for _, cfg := range configs {
		wg.Add(1)
		go func(config *nodeconfig.Config) {
			defer wg.Done()

			if err := cm.sem.Acquire(ctx, 1); err != nil {
				errCh <- fmt.Errorf("error acquring semaphore: %w", err)
				return
			}
			defer cm.sem.Release(1)

			select {
			case <-deploymentCtx.Done():
				errCh <- deploymentCtx.Err()
				return
			default:
				err := cm.deployConfig(deploymentCtx, config)
				if err != nil {
					deploymentCancel()
				}
				errCh <- err
				return
			}
		}(cfg)
	}

	wg.Wait()
	close(errCh)

	if err := cm.checkErrors(errCh); err != nil {
		return fmt.Errorf("errors occurred: %w", err)
	}

	if err := cm.setProcessStatus(ctx, nodeconfig.StatusProvisioned); err != nil {
		return fmt.Errorf("error setting process status: %w", err)
	}

	return nil
}

func (cm *ConfigManager) checkErrors(errCh chan error) error {
	errCnt := 0
	var firstErr error
	for err := range errCh {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if !errors.Is(err, context.Canceled) {
				cm.logger.Error(err, "depoyment error")
			}
			errCnt++
		}
	}

	if errCnt > 0 {
		return fmt.Errorf("%d error(s) occurred while processing configs, please check the logs for details: first known error: %w", errCnt, firstErr)
	}

	return nil
}

// nolint: contextcheck
func (cm *ConfigManager) deployConfig(ctx context.Context, cfg *nodeconfig.Config) error {
	if cfg.GetActive() {
		cfgContext, cfgCancel := context.WithTimeout(ctx, cm.timeout)
		cfgContext = context.WithValue(cfgContext, nodeconfig.ParentCtx, ctx)
		cfg.SetCancelFunc(cfgCancel)

		cm.logger.Info("processing config", "name", cfg.GetName())
		if err := cfg.Deploy(cfgContext, cm.client, cm.logger, cm.timeout); err != nil {
			// we invalidate config on new, separate context, so invalidadion won't get cancelled
			// if node update limit is set to more than 1.
			invalidationCtx, invalidationCancel := context.WithTimeout(context.Background(), cm.timeout)
			defer invalidationCancel()
			if err := cfg.CrateInvalid(invalidationCtx, cm.client); err != nil {
				return fmt.Errorf("error creating invalid config object: %w", err)
			}
			return fmt.Errorf("error deploying config %s: %w", cfg.GetName(), err)
		}
		cm.logger.Info("deployed", "name", cfg.GetName())
	}
	return nil
}

func (cm *ConfigManager) validateConfigs(configs []*nodeconfig.Config) error {
	cm.logger.Info("validating configs...")
	for _, cfg := range configs {
		if !cfg.GetActive() {
			continue
		}

		next := cfg.GetNext()
		if next != nil {
			cm.logger.Info("next config", "config", *next)
		}
		invalid := cfg.GetInvalid()
		if invalid != nil {
			cm.logger.Info("invalid config", "config", *invalid)
		}

		if invalid != nil && next != nil {
			if next.IsEqual(invalid) {
				return fmt.Errorf("config for node %s results in invalid config", cfg.GetName())
			}
		}
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
		return fmt.Errorf("error getting process object: %w", err)
	}
	process.Spec.State = status
	if err := cm.client.Update(ctx, process); err != nil {
		return fmt.Errorf("error updating process object: %w", err)
	}
	cm.logger.Info("process status set", "status", status)
	return nil
}

func (cm *ConfigManager) DeployConfigs(ctx context.Context) error {
	cm.logger.Info("deploying configs ...")
	toDeploy := cm.configs.GetSlice()

	if err := cm.Deploy(ctx, toDeploy); err != nil {
		return fmt.Errorf("error deploying configs: %w", err)
	}

	return nil
}

func (cm *ConfigManager) RestoreBackup(ctx context.Context) error {
	cm.logger.Info("restoring backup...")
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

	cm.logger.Info("backup restored")
	return nil
}

func (cm *ConfigManager) getProcess(ctx context.Context) (*v1alpha1.NodeConfigProcess, error) {
	process := &v1alpha1.NodeConfigProcess{
		ObjectMeta: metav1.ObjectMeta{
			Name: processName,
		},
	}
	if err := cm.client.Get(ctx, client.ObjectKeyFromObject(process), process); err != nil {
		return process, fmt.Errorf("error getting process object: %w", err)
	}
	return process, nil
}

// DirtyStartup will load all previously deployed NodeConfigs into current leader
func (cm *ConfigManager) DirtyStartup(ctx context.Context) error {
	process, err := cm.getProcess(ctx)
	if err != nil {
		// process object does not exists - there was no operator running on this cluster before
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("error getting process object: %w", err)
	}

	cm.logger.Info("previous leader left cluster in state", "state", process.Spec.State)
	cm.logger.Info("using data left by previous leader...")

	// get all known backup data and load it into config manager memory
	nodes, err := reconciler.ListNodes(ctx, cm.client)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	knownConfigs := &v1alpha1.NodeConfigList{}
	if err := cm.client.List(ctx, knownConfigs); err != nil {
		return fmt.Errorf("error listing NodeConfigs: %w", err)
	}

	for name := range nodes {
		var current *v1alpha1.NodeConfig
		var backup *v1alpha1.NodeConfig
		var invalid *v1alpha1.NodeConfig
		for j := range knownConfigs.Items {
			if knownConfigs.Items[j].Name == name {
				cm.logger.Info("found current config", "node", name)
				current = &knownConfigs.Items[j]
			}
			if knownConfigs.Items[j].Name == name+nodeconfig.BackupSuffix {
				cm.logger.Info("found backup config", "node", name)
				backup = &knownConfigs.Items[j]
			}
			if knownConfigs.Items[j].Name == name+nodeconfig.InvalidSuffix {
				cm.logger.Info("found invalid config", "node", name)
				invalid = &knownConfigs.Items[j]
			}
		}
		cfg := nodeconfig.New(name, current, backup, invalid)
		if backup != nil {
			cfg.SetDeployed(true)
		}
		cm.configs.Add(name, cfg)
	}

	// prevouos leader left cluster in provisioning state - restore known backups
	if process.Spec.State == nodeconfig.StatusProvisioning {
		if err := cm.RestoreBackup(ctx); err != nil {
			return fmt.Errorf("error restoring backup: %w", err)
		}
	}
	return nil
}
