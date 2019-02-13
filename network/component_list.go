package network

import (
	"reflect"
	"sort"
)

// ComponentInfo wraps a priority level with a Component interface.
type ComponentInfo struct {
	Priority  int
	Component ComponentInterface
}

// ComponentList holds a statically-typed sorted map of Components
// registered on Noise.
type ComponentList struct {
	keys   map[reflect.Type]*ComponentInfo
	values []*ComponentInfo
}

// NewComponentList creates a new instance of a sorted Component list.
func NewComponentList() *ComponentList {
	return &ComponentList{
		keys:   make(map[reflect.Type]*ComponentInfo),
		values: make([]*ComponentInfo, 0),
	}
}

// SortByPriority sorts the Components list by each Components priority.
func (m *ComponentList) SortByPriority() {
	sort.SliceStable(m.values, func(i, j int) bool {
		return m.values[i].Priority < m.values[j].Priority
	})
}

// PutInfo places a new Components info onto the list.
func (m *ComponentList) PutInfo(Component *ComponentInfo) bool {
	ty := reflect.TypeOf(Component.Component)
	if _, ok := m.keys[ty]; ok {
		return false
	}
	m.keys[ty] = Component
	m.values = append(m.values, Component)
	return true
}

// Put places a new Component with a set priority onto the list.
func (m *ComponentList) Put(priority int, Component ComponentInterface) bool {
	return m.PutInfo(&ComponentInfo{
		Priority:  priority,
		Component: Component,
	})
}

// Len returns the number of Components in the Component list.
func (m *ComponentList) Len() int {
	return len(m.keys)
}

// GetInfo gets the priority and Component interface given a Component ID. Returns nil if not exists.
func (m *ComponentList) GetInfo(withTy interface{}) (*ComponentInfo, bool) {
	item, ok := m.keys[reflect.TypeOf(withTy)]
	return item, ok
}

// Get returns the Component interface given a Component ID. Returns nil if not exists.
func (m *ComponentList) Get(withTy interface{}) (ComponentInterface, bool) {
	if info, ok := m.GetInfo(withTy); ok {
		return info.Component, true
	} else {
		return nil, false
	}
}

// Each goes through every Component in ascending order of priority of the Component list.
func (m *ComponentList) Each(f func(value ComponentInterface)) {
	for _, item := range m.values {
		f(item.Component)
	}
}
