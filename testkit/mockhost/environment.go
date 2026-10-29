// SPDX-License-Identifier: Apache-2.0

package mockhost

import (
	"encoding/json"
	"errors"
	"github.com/arvel-dev/arvel-sdk/protocol"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
)

// EnvironmentOptions supplies borrowed paths and initialization defaults.
type EnvironmentOptions struct {
	Initialize protocol.InitMeta
	Workspace  string
}

// Environment is a stable test context. Only its allocated root is removed.
type Environment struct {
	mu              sync.Mutex
	meta            protocol.InitMeta
	workspace, root string
	users           int
	closed          bool
}

// NewEnvironment allocates private missing paths, preserving explicit metadata.
func NewEnvironment(opts EnvironmentOptions) (*Environment, error) {
	if _, err := json.Marshal(opts.Initialize); err != nil {
		return nil, errors.New("mockhost: invalid initialization metadata")
	}
	e := &Environment{meta: cloneMeta(opts.Initialize), workspace: opts.Workspace}
	if !filepath.IsAbs(os.TempDir()) {
		return nil, errors.New("mockhost: temporary directory must be absolute")
	}
	root, err := os.MkdirTemp("", "arvel-host-")
	if err != nil {
		return nil, errors.New("mockhost: allocate environment")
	}
	e.root = root
	fail := func() (*Environment, error) {
		_ = os.RemoveAll(root)
		return nil, errors.New("mockhost: allocate environment directories")
	}
	if e.meta.DataDir == "" {
		e.meta.DataDir = filepath.Join(root, "data")
		if os.Mkdir(e.meta.DataDir, 0700) != nil {
			return fail()
		}
	}
	if e.workspace == "" {
		e.workspace = filepath.Join(root, "workspace")
		if os.Mkdir(e.workspace, 0700) != nil {
			return fail()
		}
	}
	if e.meta.PluginAPI == "" {
		e.meta.PluginAPI = protocol.PluginAPI
	}
	if e.meta.Connection.ID == "" {
		e.meta.Connection.ID = "test-" + filepath.Base(root)
	}
	if e.meta.Platform.OS == "" {
		e.meta.Platform.OS = runtime.GOOS
	}
	if e.meta.Platform.Arch == "" {
		e.meta.Platform.Arch = runtime.GOARCH
	}
	return e, nil
}
func cloneMeta(m protocol.InitMeta) protocol.InitMeta {
	m.Connection.Config = cloneMap(m.Connection.Config)
	if m.Connection.Secrets != nil {
		cp := make(map[string]string, len(m.Connection.Secrets))
		for k, v := range m.Connection.Secrets {
			cp[k] = v
		}
		m.Connection.Secrets = cp
	}
	if m.Sandbox != nil {
		cp := *m.Sandbox
		m.Sandbox = &cp
	}
	return m
}
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	o := make(map[string]any, len(m))
	for k, v := range m {
		o[k] = cloneValue(v)
	}
	return o
}
func cloneValue(v any) any {
	if v == nil {
		return nil
	}
	return cloneJSONValue(reflect.ValueOf(v)).Interface()
}
func cloneJSONValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneJSONValue(v.Elem()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneJSONValue(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			out.SetMapIndex(it.Key(), cloneJSONValue(it.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneJSONValue(v.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath == "" {
				out.Field(i).Set(cloneJSONValue(v.Field(i)))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneJSONValue(v.Index(i)))
		}
		return out
	default:
		return v
	}
}

// Initialize returns a cloned snapshot of the defaults.
func (e *Environment) Initialize() protocol.InitMeta { return cloneMeta(e.meta) }

// Workspace returns the stable default session cwd.
func (e *Environment) Workspace() string { return e.workspace }

// Root returns the owned scratch directory for test resources.
func (e *Environment) Root() string { return e.root }
func (e *Environment) acquire() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errors.New("mockhost: environment is closed")
	}
	e.users++
	return nil
}
func (e *Environment) release()    { e.mu.Lock(); e.users--; e.mu.Unlock() }
func (e *Environment) inUse() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.users != 0 }

// Close removes owned paths only after all drivers have stopped.
func (e *Environment) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.users != 0 {
		return errors.New("mockhost: environment is still in use")
	}
	if e.closed {
		return nil
	}
	if os.RemoveAll(e.root) != nil {
		return errors.New("mockhost: remove owned environment")
	}
	e.closed = true
	return nil
}
func (e *Environment) fill(m protocol.InitMeta) protocol.InitMeta {
	m = cloneMeta(m)
	b := e.Initialize()
	if m.PluginAPI == "" {
		m.PluginAPI = b.PluginAPI
	}
	if m.DataDir == "" {
		m.DataDir = b.DataDir
	}
	if m.Connection.ID == "" {
		m.Connection.ID = b.Connection.ID
	}
	if m.Connection.Kind == "" {
		m.Connection.Kind = b.Connection.Kind
	}
	sameKind := m.Connection.Kind == b.Connection.Kind || m.Connection.Kind == ""
	if m.Connection.Config == nil && sameKind {
		m.Connection.Config = b.Connection.Config
	}
	if m.Connection.Secrets == nil && sameKind {
		m.Connection.Secrets = b.Connection.Secrets
	}
	if m.Platform == (protocol.PlatformInfo{}) {
		m.Platform = b.Platform
	}
	if m.Locale == "" {
		m.Locale = b.Locale
	}
	if m.Platform.OS == "" {
		m.Platform.OS = b.Platform.OS
	}
	if m.Platform.Arch == "" {
		m.Platform.Arch = b.Platform.Arch
	}
	if m.Sandbox == nil {
		m.Sandbox = b.Sandbox
	}
	return m
}
