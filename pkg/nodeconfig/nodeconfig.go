package nodeconfig

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	statusProvisioning = "provisioning"
	statusInvalid      = "invalid"
	statusProvisioned  = "provisioned"
	statusEmpty        = ""

	DefaultTimeout         = "60s"
	DefaultNodeUpdateLimit = 1
	defaultCooldownTime    = 100 * time.Millisecond

	invalidSuffix = "-invalid"
	backupSuffix  = "-backup"
)

type Config struct {
	name       string
	current    *v1alpha1.NodeConfig
	next       *v1alpha1.NodeConfig
	backup     *v1alpha1.NodeConfig
	invalid    *v1alpha1.NodeConfig
	cancelFunc context.CancelFunc
	mtx        sync.RWMutex
	active     atomic.Bool
	deployed   atomic.Bool
}

func New(name string, current, backup, invalid *v1alpha1.NodeConfig) *Config {
	nc := NewEmpty(name)
	nc.current = current
	nc.backup = backup
	nc.invalid = invalid
	return nc
}

func (nc *Config) SetCancelFunc(f context.CancelFunc) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.cancelFunc = f
}

func (nc *Config) GetCancelFunc() context.CancelFunc {
	nc.mtx.RLock()
	defer nc.mtx.RUnlock()
	return nc.cancelFunc
}

func (nc *Config) GetName() string {
	nc.mtx.RLock()
	defer nc.mtx.RUnlock()
	return nc.name
}

func (nc *Config) GetActive() bool {
	nc.mtx.RLock()
	defer nc.mtx.RUnlock()
	return nc.active.Load()
}

func (nc *Config) SetActive(value bool) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.active.Store(value)
}

func (nc *Config) SetDeployed(value bool) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.deployed.Store(value)
}

func (nc *Config) GetDeployed() bool {
	nc.mtx.RLock()
	defer nc.mtx.RUnlock()
	return nc.deployed.Load()
}

func (nc *Config) GetCurrentConfigStatus() string {
	nc.mtx.RLock()
	defer nc.mtx.RUnlock()
	return nc.current.Status.ConfigStatus
}

func (nc *Config) Update(current, backup, invalid *v1alpha1.NodeConfig) {
	nc.UpdateCurrent(current)
	nc.UpdateBackup(backup)
	nc.UpdateInvalid(invalid)
}

func (nc *Config) UpdateCurrent(current *v1alpha1.NodeConfig) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.current = current
}

func (nc *Config) UpdateBackup(backup *v1alpha1.NodeConfig) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.backup = backup
}

func (nc *Config) UpdateInvalid(invalid *v1alpha1.NodeConfig) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.backup = invalid
}

func (nc *Config) UpdateNext(next *v1alpha1.NodeConfig) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	nc.next = next
}

func NewEmpty(name string) *Config {
	nc := &Config{
		name:    name,
		current: &v1alpha1.NodeConfig{},
	}
	nc.active.Store(true)
	return nc
}

func (nc *Config) Deploy(ctx context.Context, c client.Client) error {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()

	if !nc.active.Load() {
		return nil
	}
	if nc.current != nil && nc.current.Name != "" {
		// config already exists - update
		// check if new config is equal to existing config
		// if so, skip the update as nothing has to be updated
		if nc.next.IsEqual(nc.current) {
			return nil
		}
		v1alpha1.CopyNodeConfig(nc.next, nc.current, nc.name)
		if err := updateConfig(ctx, c, nc.current); err != nil {
			return fmt.Errorf("error updating NodeConfig object: %w", err)
		}
	} else {
		nc.current = v1alpha1.NewEmptyConfig(nc.name)
		v1alpha1.CopyNodeConfig(nc.next, nc.current, nc.current.Name)
		// config does not exist - create
		if err := c.Create(ctx, nc.current); err != nil {
			return fmt.Errorf("error creating NodeConfig object: %w", err)
		}
	}

	nc.deployed.Store(true)

	if err := waitForConfig(ctx, c, nc.current, statusEmpty, false); err != nil {
		return fmt.Errorf("error wating for config %s with status %s: %w", nc.name, statusEmpty, err)
	}

	if err := updateStatus(ctx, c, nc.current, statusProvisioning); err != nil {
		return fmt.Errorf("error updateing status of config %s to %s: %w", nc.name, statusProvisioning, err)
	}

	if err := waitForConfig(ctx, c, nc.current, statusProvisioning, true); err != nil {
		return fmt.Errorf("error wating for config %s with status %s: %w", nc.name, statusProvisioning, err)
	}

	return nil
}

func (nc *Config) SetBackupAsNext() {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	v1alpha1.CopyNodeConfig(nc.backup, nc.next, nc.current.Name)
}

func (nc *Config) CreateBackup(ctx context.Context, c client.Client) error {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	backupName := nc.name + backupSuffix
	createNew := false
	if nc.backup == nil {
		nc.backup = v1alpha1.NewEmptyConfig(backupName)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: backupName}, nc.backup); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("error getting backup config %s: %w", backupName, err)
		}
		createNew = true
	}

	if nc.current != nil {
		v1alpha1.CopyNodeConfig(nc.current, nc.backup, backupName)
	} else {
		v1alpha1.CopyNodeConfig(v1alpha1.NewEmptyConfig(backupName), nc.backup, backupName)
	}

	if createNew {
		if err := c.Create(ctx, nc.backup); err != nil {
			return fmt.Errorf("error creating backup config: %w", err)
		}
	} else {
		if err := c.Update(ctx, nc.backup); err != nil {
			return fmt.Errorf("error updating backup config: %w", err)
		}
	}

	return nil
}

func (nc *Config) CrateInvalid(ctx context.Context, c client.Client) error {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()
	invalidName := fmt.Sprintf("%s%s", nc.name, invalidSuffix)

	if nc.invalid == nil {
		nc.invalid = v1alpha1.NewEmptyConfig(invalidName)
	}

	if err := c.Get(ctx, types.NamespacedName{Name: invalidName}, nc.invalid); err != nil {
		if apierrors.IsNotFound(err) {
			// invalid config for the node does not exist - create new
			v1alpha1.CopyNodeConfig(nc.current, nc.invalid, invalidName)
			if err = c.Create(ctx, nc.invalid); err != nil {
				return fmt.Errorf("cannot store invalid config for node %s: %w", nc.name, err)
			}
			return nil
		}
		// other kind of error occurred - abort
		return fmt.Errorf("error getting invalid config for node %s: %w", nc.name, err)
	}

	// invalid config for the node exist - update
	v1alpha1.CopyNodeConfig(nc.current, nc.invalid, invalidName)
	if err := updateConfig(ctx, c, nc.invalid); err != nil {
		return fmt.Errorf("error updating invalid config for node %s: %w", nc.name, err)
	}

	return nil
}

func waitForConfig(ctx context.Context, c client.Client, instance *v1alpha1.NodeConfig, expectedStatus string, failIfInvalid bool) error {
	for {
		select {
		case <-ctx.Done():
			// return if context is done (e.g. cancelled)
			return fmt.Errorf("context error: %w", ctx.Err())
		default:
			err := c.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
			if err == nil {
				// Accept any status ("") or expected status
				if expectedStatus == "" || instance.Status.ConfigStatus == expectedStatus {
					return nil
				}

				// return error if status is invalid
				if failIfInvalid && instance.Status.ConfigStatus == statusInvalid {
					return fmt.Errorf("error creating NodeConfig - node %s reported state as %s", instance.Name, instance.Status.ConfigStatus)
				}
			}
			time.Sleep(defaultCooldownTime)
		}
	}
}

func updateStatus(ctx context.Context, c client.Client, config *v1alpha1.NodeConfig, status string) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("status update error: %w", ctx.Err())
		default:
			config.Status.ConfigStatus = status
			err := c.Status().Update(ctx, config)
			if err != nil {
				if apierrors.IsConflict(err) {
					// if there is a conflict, update local copy of the config
					if getErr := c.Get(ctx, client.ObjectKeyFromObject(config), config); getErr != nil {
						return fmt.Errorf("error updating status: %w", getErr)
					}
					time.Sleep(defaultCooldownTime)
					continue
				}
				return fmt.Errorf("status update error: %w", err)
			} else {
				return nil
			}
		}
	}
}

func updateConfig(ctx context.Context, c client.Client, config *v1alpha1.NodeConfig) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("status update error: %w", ctx.Err())
		default:
			if err := c.Update(ctx, config); err != nil {
				if apierrors.IsConflict(err) {
					// if there is a conflict, update local copy of the config
					if getErr := c.Get(ctx, client.ObjectKeyFromObject(config), config); getErr != nil {
						return fmt.Errorf("error updating status: %w", getErr)
					}
					time.Sleep(defaultCooldownTime)
					continue
				}
				return fmt.Errorf("status update error: %w", err)
			} else {
				return nil
			}
		}
	}
}
