package configmap

import (
	"sync"

	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
)

type ConfigMap struct {
	configs map[string]*nodeconfig.Config
	mtx     sync.RWMutex
}

func New() *ConfigMap {
	return &ConfigMap{
		configs: make(map[string]*nodeconfig.Config),
	}
}

func (cm *ConfigMap) Add(name string, config *nodeconfig.Config) {
	cm.mtx.Lock()
	defer cm.mtx.Unlock()
	cm.configs[name] = config
}

func (cm *ConfigMap) Remove(name string) {
	cm.mtx.Lock()
	defer cm.mtx.Unlock()
	delete(cm.configs, name)
}

func (cm *ConfigMap) Get(name string) *nodeconfig.Config {
	cm.mtx.RLock()
	defer cm.mtx.RUnlock()
	if cfg, exist := cm.configs[name]; exist {
		return cfg
	}
	return nil
}

func (cm *ConfigMap) GetSlice() []*nodeconfig.Config {
	cm.mtx.RLock()
	defer cm.mtx.RUnlock()
	slice := []*nodeconfig.Config{}
	for _, config := range cm.configs {
		slice = append(slice, config)
	}
	return slice
}
