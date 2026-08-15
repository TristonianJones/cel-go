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

package interpreter

import (
	"errors"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// memoryTrackPlanOption modifies the memory tracking factory associated with the MemoryObserver.
type memoryTrackPlanOption func(*memoryTrackerFactory) *memoryTrackerFactory

// MemoryTrackerFactory configures the factory method to generate a new memory-tracker
// per-evaluation.
func MemoryTrackerFactory(factory func() (*types.MemoryTracker, error)) memoryTrackPlanOption {
	return func(fac *memoryTrackerFactory) *memoryTrackerFactory {
		fac.factory = factory
		return fac
	}
}

// MemoryObserver provides an observer that tracks runtime peak memory.
func MemoryObserver(opts ...memoryTrackPlanOption) PlannerOption {
	mt := &memoryTrackerFactory{}
	for _, o := range opts {
		mt = o(mt)
	}
	return func(p *planner) (*planner, error) {
		if mt.factory == nil {
			return nil, errors.New("memory tracker factory not configured")
		}
		p.observers = append(p.observers, mt)
		p.decorators = append(p.decorators, decObserveEval(mt.Observe))
		return p, nil
	}
}

// memoryTrackerState holds the per-evaluation memory tracking state.
//
// The MemoryTracker itself is interpreter-independent; the state pairs it with the
// interpreter-specific bookkeeping needed to identify re-evaluated expression nodes
// for sampling.
type memoryTrackerState struct {
	tracker *types.MemoryTracker
	seen    map[int64]bool
}

// observe records a watermark for the value produced at the given expression id.
//
// Expression nodes observed more than once per evaluation are re-evaluated nodes, such as
// comprehension loop bodies and bind initializers, and are subject to the tracker's sample
// interval; first-time observations are always tracked.
func (s *memoryTrackerState) observe(id int64, val any) {
	if s.seen[id] {
		s.tracker.Sample(val)
		return
	}
	s.seen[id] = true
	s.tracker.Track(val)
}

// memoryTrackerFactory holds a factory for producing new MemoryTracker instances on each Eval call.
type memoryTrackerFactory struct {
	factory func() (*types.MemoryTracker, error)
}

// InitState produces a MemoryTracker and bundles it into the ExecutionFrame in a way which is
// not visible to expression evaluation.
func (mt *memoryTrackerFactory) InitState(frame *ExecutionFrame) (any, error) {
	if frame.ctx != nil && frame.ctx.memory != nil {
		return frame.ctx.memory.tracker, nil
	}
	tracker, err := mt.factory()
	if err != nil {
		return nil, err
	}
	if frame.ctx == nil {
		frame.ctx = evalContextPool.Get().(*evalContext)
	}
	frame.ctx.memory = &memoryTrackerState{tracker: tracker, seen: map[int64]bool{}}
	return tracker, nil
}

// GetState extracts the MemoryTracker from the ExecutionFrame.
func (mt *memoryTrackerFactory) GetState(frame *ExecutionFrame) any {
	if frame == nil || frame.ctx == nil || frame.ctx.memory == nil {
		return nil
	}
	return frame.ctx.memory.tracker
}

// Observe records the peak memory watermarks associated with each evaluation step.
//
// Watermarks are observed at the points where values materialize during evaluation: resolved
// attributes, function call results, constructed aggregate literals, and comprehension results.
// Since every intermediate Interpretable is observed, the inputs to a call contribute to the
// peak at the expression nodes which produced them; constants are part of the program image
// rather than runtime-materialized memory and are not observed.
func (mt *memoryTrackerFactory) Observe(vars Activation, id int64, programStep any, val ref.Val) {
	frame := AsFrame(vars)
	if frame == nil || frame.ctx == nil || frame.ctx.memory == nil {
		return
	}
	state := frame.ctx.memory
	switch programStep.(type) {
	case InterpretableAttribute, InterpretableCall, InterpretableConstructor, *evalFold:
		state.observe(id, val)
		if state.tracker.ExceedsLimit() {
			panic(EvalCancelledError{Cause: MemoryLimitExceeded, Message: "operation cancelled: memory limit exceeded"})
		}
	}
}
