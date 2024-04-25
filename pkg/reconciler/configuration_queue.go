package reconciler

import (
	"container/list"
	"fmt"
	"sync"
)

type configQueue struct {
	mtx   sync.RWMutex
	items *list.List
}

func newConfigQueue() *configQueue {
	return &configQueue{
		items: list.New().Init(),
	}
}

func (cq *configQueue) PushBack(item any) {
	cq.mtx.Lock()
	defer cq.mtx.Unlock()
	cq.items.PushBack(item)
}

func (cq *configQueue) Pop() *nodeConfiguration {
	cq.mtx.Lock()
	defer cq.mtx.Unlock()
	element := cq.items.Front()
	v := cq.items.Remove(element)
	config, ok := v.(*nodeConfiguration)
	if !ok {
		return nil
	}
	return config
}

func (cq *configQueue) Remove(name string) error {
	cq.mtx.Lock()
	defer cq.mtx.Unlock()
	for element := cq.items.Front(); element != nil; element.Next() {
		config, ok := element.Value.(*nodeConfiguration)
		if !ok {
			return fmt.Errorf("error converting list element to nodeConfiguration pointer")
		}
		if config.name == name {
			cq.items.Remove(element)
			return nil
		}
	}
	return nil
}

func (cq *configQueue) Clear() {
	cq.mtx.Lock()
	defer cq.mtx.Unlock()
	cq.items = cq.items.Init()
}

func (cq *configQueue) Len() int {
	cq.mtx.RLock()
	defer cq.mtx.RUnlock()
	return cq.items.Len()
}

func (cq *configQueue) Front() *list.Element {
	cq.mtx.RLock()
	defer cq.mtx.RUnlock()
	return cq.items.Front()
}

func (cq *configQueue) Next(element *list.Element) *list.Element {
	cq.mtx.RLock()
	defer cq.mtx.RUnlock()
	return element.Next()
}
