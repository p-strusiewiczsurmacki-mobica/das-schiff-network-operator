package configmap

import (
	"fmt"
	"sync"

	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
)

type ConfigMap struct {
	sync.Map
}

func (cm *ConfigMap) Get(name string) (*nodeconfig.Config, error) {
	cfg, ok := cm.Load(name)
	if !ok {
		return nil, nil
	}
	config, ok := cfg.(*nodeconfig.Config)
	if !ok {
		return nil, fmt.Errorf("error converting config for node %s from interface", name)
	}
	return config, nil
}

func (cm *ConfigMap) GetSlice() ([]*nodeconfig.Config, error) {
	slice := []*nodeconfig.Config{}
	var err error
	cm.Range(func(key any, value any) bool {
		name, ok := key.(string)
		if !ok {
			err = fmt.Errorf("error converting key %v to string", key)
			return false
		}

		if value == nil {
			slice = append(slice, nil)
			return true
		}

		config, ok := value.(*nodeconfig.Config)
		if !ok {
			err = fmt.Errorf("error converting config %s from interface", name)
			return false
		}
		slice = append(slice, config)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("error converting config map to slice: %w", err)
	}
	return slice, nil
}
