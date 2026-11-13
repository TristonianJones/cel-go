// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package runtime provides a decoupled CEL evaluation environment and program planner that does
// not depend on the CEL parser, type-checker, or AST optimizer.
package runtime

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/containers"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"
)

// Env encapsulates the runtime context necessary to plan and evaluate CEL ASTs.
type Env struct {
	Container *containers.Container
	adapter   types.Adapter
	provider  types.Provider
	bindings  map[string]*functions.Overload

	jsonFieldNames         bool
	errorOnBadPresenceTest bool
	regexProgramSizeLimit  int

	costConfig *costConfig
	costModel  []cost.TrackerOption

	progOpts []ProgramOption

	// Flags for copy-on-write behavior with Env.Extend.
	funcsShared bool
	costShared  bool
	parent      *Env

	sharedDispatcher interpreter.Dispatcher
	dispOnce         sync.Once
	hasAsync         bool
	dispErr          error
}

type costConfig struct {
	strategy cost.SizingStrategy
	models   []cost.OverloadModel
	version  *uint32
}

func (c *costConfig) copy() *costConfig {
	return &costConfig{
		strategy: c.strategy,
		models:   slices.Clone(c.models),
		version:  c.version,
	}
}

func (c *costConfig) buildTrackerOptions() []cost.TrackerOption {
	var opts []cost.TrackerOption
	if len(c.models) > 0 {
		opts = append(opts, cost.Model(c.models...))
	}
	if c.strategy != nil {
		opts = append(opts, cost.WithSizingStrategy(c.strategy))
	}
	if c.version != nil {
		opts = append(opts, cost.ModelVersion(*c.version))
	}
	return opts
}

var (
	stdEnvOnce sync.Once
	stdEnv     *Env
	stdEnvErr  error
)

func getStdEnv() (*Env, error) {
	stdEnvOnce.Do(func() {
		stdEnv, stdEnvErr = NewCustomEnv(StdLib())
	})
	return stdEnv, stdEnvErr
}

// NewEnv creates a runtime environment configured with the standard library of CEL functions.
//
// See the EnvOption helper functions for the options that can be used to configure the environment.
func NewEnv(opts ...EnvOption) (*Env, error) {
	env, err := getStdEnv()
	if err != nil {
		return nil, err
	}
	return env.Extend(opts...)
}

// NewCustomEnv creates a custom runtime environment which is not automatically configured with the
// standard library of CEL functions.
func NewCustomEnv(opts ...EnvOption) (*Env, error) {
	registry, err := types.NewRegistry()
	if err != nil {
		return nil, err
	}
	return (&Env{
		Container:  containers.DefaultContainer,
		adapter:    registry,
		provider:   registry,
		bindings:   map[string]*functions.Overload{},
		progOpts:   []ProgramOption{},
		costConfig: &costConfig{},
	}).configure(opts)
}

func (e *Env) configure(opts []EnvOption) (*Env, error) {
	var err error
	for _, opt := range opts {
		e, err = opt(e)
		if err != nil {
			return nil, err
		}
	}
	if e.jsonFieldNames {
		if err := e.applyJSONFieldNames(); err != nil {
			return nil, err
		}
	}
	if !e.costShared {
		e.costModel = e.costConfig.buildTrackerOptions()
	}
	return e, nil
}

// Extend creates a new runtime environment by extending the current one with additional options.
//
// The extended Env does not share mutable state with the original. If CustomTypeAdapter or
// CustomTypeProvider was supplied with a *types.Registry, it is copied via Registry.Copy().
func (e *Env) Extend(opts ...EnvOption) (*Env, error) {
	adapter := e.adapter
	provider := e.provider
	adapterReg, isAdapterReg := e.adapter.(*types.Registry)
	providerReg, isProviderReg := e.provider.(*types.Registry)
	if isAdapterReg && isProviderReg {
		reg := providerReg.Copy()
		provider = reg
		if adapterReg == providerReg {
			adapter = reg
		} else {
			adapter = adapterReg.Copy()
		}
	} else if isProviderReg {
		provider = providerReg.Copy()
	} else if isAdapterReg {
		adapter = adapterReg.Copy()
	}

	ext := &Env{
		parent:                 e,
		Container:              e.Container,
		adapter:                adapter,
		provider:               provider,
		bindings:               e.bindings,
		jsonFieldNames:         e.jsonFieldNames,
		errorOnBadPresenceTest: e.errorOnBadPresenceTest,
		regexProgramSizeLimit:  e.regexProgramSizeLimit,
		costConfig:             e.costConfig,
		costModel:              e.costModel,
		progOpts:               slices.Clone(e.progOpts),
		funcsShared:            true,
		costShared:             true,
	}
	return ext.configure(opts)
}

func (e *Env) ensureMutableBindings() {
	if e.funcsShared {
		e.bindings = maps.Clone(e.bindings)
		e.funcsShared = false
	}
}

func (e *Env) ensureMutableCostConfig() {
	if e.costShared {
		e.costConfig = e.costConfig.copy()
		e.costShared = false
	}
}

// Program generates an evaluable instance of the AST within the runtime environment.
func (e *Env) Program(a *ast.AST, opts ...ProgramOption) (Program, error) {
	optSet := e.progOpts
	if len(opts) != 0 {
		merged := make([]ProgramOption, 0, len(e.progOpts)+len(opts))
		merged = append(merged, e.progOpts...)
		merged = append(merged, opts...)
		optSet = merged
	}
	return newProgram(e, a, optSet)
}

func (e *Env) initDispatcher() (interpreter.Dispatcher, bool, error) {
	e.dispOnce.Do(func() {
		if e.parent != nil && e.funcsShared {
			d, hasAsync, err := e.parent.initDispatcher()
			e.sharedDispatcher = d
			e.hasAsync = hasAsync
			e.dispErr = err
			return
		}
		hasAsync := false
		overloads := make([]*functions.Overload, 0, len(e.bindings))
		for _, b := range e.bindings {
			if b.Async != nil {
				hasAsync = true
			}
			overloads = append(overloads, b)
		}
		d := interpreter.NewDispatcher()
		e.dispErr = d.Add(overloads...)
		e.sharedDispatcher = d
		e.hasAsync = hasAsync
	})
	return e.sharedDispatcher, e.hasAsync, e.dispErr
}

// CELTypeAdapter returns the types.Adapter configured for the environment.
func (e *Env) CELTypeAdapter() types.Adapter {
	return e.adapter
}

// CELTypeProvider returns the types.Provider configured for the environment.
func (e *Env) CELTypeProvider() types.Provider {
	return e.provider
}

// TypeAdapter returns the ref.TypeAdapter configured for the environment.
//
// Deprecated: use CELTypeAdapter()
func (e *Env) TypeAdapter() ref.TypeAdapter {
	return e.adapter
}

// TypeProvider returns the ref.TypeProvider configured for the environment.
//
// Deprecated: use CELTypeProvider()
func (e *Env) TypeProvider() ref.TypeProvider {
	if legacyProvider, ok := e.provider.(ref.TypeProvider); ok {
		return legacyProvider
	}
	return &interopLegacyTypeProvider{Provider: e.provider}
}

func (e *Env) applyJSONFieldNames() error {
	reg, isReg := e.provider.(*types.Registry)
	if !isReg {
		return fmt.Errorf("JSONFieldNames() option is only compatible with *types.Registry providers")
	}
	return reg.WithJSONFieldNames(true)
}
