package reconciler

import (
	"context"
	"fmt"
	"sync"

	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type nodeConfiguration struct {
	name      string
	current   *v1alpha1.NodeConfig
	next      *v1alpha1.NodeConfig
	backup    *v1alpha1.NodeConfig
	invalid   *v1alpha1.NodeConfig
	mtx       sync.Mutex
	lastError error
	ctxCancel context.CancelFunc
}

func newNodeConfiguration(name string, current, backup, invalid *v1alpha1.NodeConfig) *nodeConfiguration {
	nc := newEmptyNodeConfiguration(name)
	nc.current = current
	nc.backup = backup
	nc.invalid = invalid
	return nc
}

func (nc *nodeConfiguration) Update(current, backup, invalid *v1alpha1.NodeConfig) {
	nc.mtx.Lock()
	defer nc.mtx.Unlock()

	nc.current = current
	nc.backup = backup
	nc.invalid = invalid
}

func newEmptyNodeConfiguration(name string) *nodeConfiguration {
	return &nodeConfiguration{
		name:    name,
		current: &v1alpha1.NodeConfig{},
		backup:  &v1alpha1.NodeConfig{},
		invalid: &v1alpha1.NodeConfig{},
		next:    &v1alpha1.NodeConfig{},
	}
}

func (nc *nodeConfiguration) deploy(ctx context.Context, c client.Client) (bool, error) {
	if nc.current != nil && nc.current.Name != "" {
		// config already exists - update
		// check if new config is equal to existing config
		// if so, skip the update as nothing has to be updated
		if nc.next.IsEqual(nc.current) {
			return false, nil
		}
		v1alpha1.CopyNodeConfig(nc.next, nc.current, nc.name)
		if err := c.Update(ctx, nc.current); err != nil {
			return true, fmt.Errorf("error updating NodeConfig object: %w", err)
		}
	} else {
		nc.current = v1alpha1.NewEmptyConfig(nc.name)
		v1alpha1.CopyNodeConfig(nc.next, nc.current, nc.current.Name)
		// config does not exist - create
		if err := c.Create(ctx, nc.current); err != nil {
			return true, fmt.Errorf("error creating NodeConfig object: %w", err)
		}
	}
	return true, nil
}

func (nc *nodeConfiguration) createBackup(ctx context.Context, c client.Client) error {
	backupName := nc.name + backupSuffix
	createNew := false
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

func (nc *nodeConfiguration) crateInvalid(ctx context.Context, c client.Client) error {
	invalidName := fmt.Sprintf("%s%s", nc.name, invalidSuffix)

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
	if err := c.Update(ctx, nc.invalid); err != nil {
		return fmt.Errorf("error updating invalid config for node %s: %w", nc.name, err)
	}

	return nil
}

func (nc *nodeConfiguration) saveError(text string, err error) {
	nc.lastError = fmt.Errorf("%s: %w", text, err)
}
