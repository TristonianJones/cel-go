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

package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cel.dev/cel-go/cel/async"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"
)

// Program is an evaluable view of an AST.
type Program interface {
	// Eval returns the result of an evaluation of the AST and environment against the input vars.
	//
	// The vars value may either be an `Activation` or a `map[string]any`.
	//
	// If the `OptTrackState`, `OptTrackCost`, `OptTrackMemory`, `OptTraceIterations`, or
	// `OptExhaustiveEval` flags are used, the `details` response will be non-nil.
	Eval(any) (ref.Val, *EvalDetails, error)

	// ContextEval evaluates the program with a set of input variables and a context object in order
	// to support cancellation and timeouts. This method must be used in conjunction with the
	// InterruptCheckFrequency() option for cancellation interrupts to impact evaluation.
	ContextEval(context.Context, any) (ref.Val, *EvalDetails, error)

	// ConcurrentEval evaluates the program concurrently, returning a channel that will receive
	// the final EvalResult when all asynchronous operations complete, or the context expires.
	ConcurrentEval(context.Context, any) <-chan EvalResult
}

// Activation used to resolve identifiers by name and references by id.
type Activation = interpreter.Activation

// NewActivation returns an activation based on a map-based binding where the map keys are
// expected to be qualified names used with ResolveName calls.
func NewActivation(bindings any) (Activation, error) {
	return interpreter.NewActivation(bindings)
}

// PartialActivation extends the Activation interface with a set of unknown AttributePatterns.
type PartialActivation = interpreter.PartialActivation

// NoVars returns an empty Activation.
func NoVars() Activation {
	return interpreter.EmptyActivation()
}

// PartialVars returns a PartialActivation which contains variables and a set of AttributePattern
// values that indicate variables or parts of variables whose value is not yet known.
func PartialVars(vars any, unknowns ...*AttributePatternType) (PartialActivation, error) {
	return interpreter.NewPartialActivation(vars, unknowns...)
}

// FunctionActivation extends the Activation interface with implementations for the functions
// declared with a late binding.
type FunctionActivation = interpreter.FunctionActivation

// LateBoundFunction is the signature required of all late-bound function implementations.
type LateBoundFunction = functions.LateBoundOp

// FunctionVars returns a FunctionActivation combining the variable bindings from `vars` with
// the late-bound function implementations from `funcs`.
func FunctionVars(vars any, funcs map[string]LateBoundFunction) (FunctionActivation, error) {
	return interpreter.NewFunctionActivation(vars, funcs)
}

// AttributePattern returns an AttributePattern that matches a top-level variable.
func AttributePattern(varName string) *AttributePatternType {
	return interpreter.NewAttributePattern(varName)
}

// AttributePatternType represents a top-level variable with an optional set of qualifier patterns.
type AttributePatternType = interpreter.AttributePattern

// EvalDetails holds additional information observed during the Eval() call.
type EvalDetails struct {
	state       interpreter.EvalState
	costTracker *cost.Tracker
	memTracker  *types.MemoryTracker
}

// State of the evaluation, non-nil if OptTrackState, OptTraceIterations, or OptExhaustiveEval is
// specified within EvalOptions.
func (ed *EvalDetails) State() interpreter.EvalState {
	if ed == nil {
		return interpreter.NewEvalState()
	}
	return ed.state
}

// ActualCost returns the tracked cost through the course of execution when CostTracking is enabled.
// Otherwise, returns nil.
func (ed *EvalDetails) ActualCost() *uint64 {
	if ed == nil || ed.costTracker == nil {
		return nil
	}
	cost := ed.costTracker.ActualCost()
	return &cost
}

// PeakMemory returns the peak memory watermark observed through the course of execution when
// MemoryTracking is enabled. Otherwise, returns nil.
func (ed *EvalDetails) PeakMemory() *uint32 {
	if ed == nil || ed.memTracker == nil {
		return nil
	}
	peak := ed.memTracker.Peak()
	return &peak
}

// EvalResult encapsulates the response from a ConcurrentEval call.
type EvalResult struct {
	Val         ref.Val
	EvalDetails *EvalDetails
	Err         error
}

func captureCostTracker(det *EvalDetails, frame *interpreter.ExecutionFrame) *EvalDetails {
	tracker := frame.CostTracker()
	if tracker == nil {
		return det
	}
	if det == nil {
		det = &EvalDetails{}
	}
	det.costTracker = tracker
	return det
}

type prog struct {
	*Env
	evalOpts                EvalOption
	defaultVars             Activation
	baseDispatcher          interpreter.Dispatcher
	progFuncs               []*functions.Overload
	dispatcher              interpreter.Dispatcher
	interpreter             interpreter.Interpreter
	interruptCheckFrequency uint

	plannerOptions     []interpreter.PlannerOption
	optimize           func(ast.Expr) []interpreter.PlannerOption
	regexOptimizations []*interpreter.RegexOptimization

	interpretable     interpreter.InterpretableV2
	observable        *interpreter.ObservableInterpretable
	callCostEstimator cost.ActualCostEstimator
	costOptions       []cost.TrackerOption
	costLimit         *uint64
	costObserver      func(*prog) (interpreter.PlannerOption, error)
	memoryOptions     []types.MemoryTrackerOption
	memoryLimit       *uint32
	memObserver       func(*prog) interpreter.PlannerOption

	hasAsync bool

	drainStrategy             async.DrainStrategy
	asyncObserver             async.Observer
	asyncCompletionBufferSize int
	asyncMaxConcurrency       int
}

// scanOptTargets walks the AST once and reports whether the Optimize() decorator (needOpt) and the
// regex-constant compiler (needRegex) have any target node present.
func scanOptTargets(root ast.Expr) (needOpt, needRegex bool) {
	ast.PostOrderVisit(root, ast.NewExprVisitor(func(e ast.Expr) {
		switch e.Kind() {
		case ast.ListKind, ast.MapKind:
			needOpt = true
		case ast.CallKind:
			switch fn := e.AsCall().FunctionName(); {
			case fn == overloads.Matches:
				needRegex = true
			case fn == operators.In || fn == operators.OldIn:
				needOpt = true
			case overloads.IsTypeConversionFunction(fn):
				needOpt = true
			}
		}
	}))
	return
}

func newProgram(e *Env, a *ast.AST, opts []ProgramOption) (Program, error) {
	p := &prog{
		Env:            e,
		plannerOptions: []interpreter.PlannerOption{},
		costOptions:    []cost.TrackerOption{},
		drainStrategy:  async.DrainReady(100 * time.Microsecond),
	}
	var err error
	for _, opt := range opts {
		p, err = opt(p)
		if err != nil {
			return nil, err
		}
	}

	baseDisp := p.baseDispatcher
	if baseDisp == nil {
		var dispHasAsync bool
		baseDisp, dispHasAsync, err = e.initDispatcher()
		if err != nil {
			return nil, err
		}
		p.hasAsync = p.hasAsync || dispHasAsync
	}
	disp := interpreter.ExtendDispatcher(baseDisp)
	if len(p.progFuncs) > 0 {
		if err := disp.Add(p.progFuncs...); err != nil {
			return nil, err
		}
	}
	p.dispatcher = disp

	var attrFactory interpreter.AttributeFactory
	attrFactorOpts := []interpreter.AttrFactoryOption{
		interpreter.EnableErrorOnBadPresenceTest(e.errorOnBadPresenceTest),
	}
	if a.SourceInfo().HasExtension("json_name", ast.NewExtensionVersion(1, 1)) {
		if !e.jsonFieldNames {
			return nil, errors.New("the AST extension 'json_name' requires the option cel.JSONFieldNames(true)")
		}
	}
	if p.evalOpts&OptPartialEval == OptPartialEval {
		attrFactory = interpreter.NewPartialAttributeFactory(e.Container, e.adapter, e.provider, attrFactorOpts...)
	} else {
		attrFactory = interpreter.NewAttributeFactory(e.Container, e.adapter, e.provider, attrFactorOpts...)
	}
	p.interpreter = interpreter.NewInterpreter(disp, e.Container, e.provider, e.adapter, attrFactory)

	plannerOptions := make([]interpreter.PlannerOption, len(p.plannerOptions))
	copy(plannerOptions, p.plannerOptions)

	if p.interruptCheckFrequency > 0 {
		plannerOptions = append(plannerOptions, interpreter.InterruptableEval())
	}
	if p.optimize != nil {
		plannerOptions = append(plannerOptions, p.optimize(a.Expr())...)
	}
	if len(p.regexOptimizations) > 0 {
		plannerOptions = append(plannerOptions, interpreter.CompileRegexConstants(p.regexOptimizations...))
	}
	if limit := e.regexProgramSizeLimit; limit > 0 {
		plannerOptions = append(plannerOptions, interpreter.RegexProgramSizeLimit(limit))
	}

	if p.evalOpts&(OptExhaustiveEval|OptTrackState|OptTrackCost|OptTrackMemory) != 0 {
		var observers []interpreter.PlannerOption
		if p.evalOpts&(OptExhaustiveEval|OptTrackState) != 0 {
			if p.evalOpts&OptTraceIterations == OptTraceIterations {
				observers = append(observers,
					interpreter.IterationTracing(),
					interpreter.EvalStateObserver(interpreter.EvalStateFactory(func() interpreter.EvalState {
						return interpreter.NewTracedEvalState()
					})))
			} else {
				observers = append(observers, interpreter.EvalStateObserver())
			}
		}
		if p.costObserver != nil {
			costOpt, err := p.costObserver(p)
			if err != nil {
				return nil, err
			}
			plannerOptions = append(plannerOptions, costOpt)
		}
		if p.memObserver != nil {
			observers = append(observers, p.memObserver(p))
		}
		if p.evalOpts&OptExhaustiveEval == OptExhaustiveEval {
			plannerOptions = append(plannerOptions,
				append([]interpreter.PlannerOption{interpreter.ExhaustiveEval()}, observers...)...)
		} else if len(observers) > 0 {
			plannerOptions = append(plannerOptions, observers...)
		}
	}
	return p.initInterpretable(a, plannerOptions)
}

func (p *prog) initInterpretable(a *ast.AST, plannerOptions []interpreter.PlannerOption) (*prog, error) {
	interpretable, err := p.interpreter.NewInterpretable(a, plannerOptions...)
	if err != nil {
		return nil, err
	}
	p.interpretable = interpretable
	if oi, ok := interpretable.(*interpreter.ObservableInterpretable); ok {
		p.observable = oi
	}
	return p, nil
}

var errAsyncRequiresConcurrentEval = errors.New("expression contains asynchronous function calls; use ConcurrentEval")

// Eval implements the Program interface method.
func (p *prog) Eval(input any) (out ref.Val, det *EvalDetails, err error) {
	if p == nil {
		return nil, nil, errors.New("program is nil")
	}
	if p.hasAsync {
		return nil, nil, errAsyncRequiresConcurrentEval
	}
	var frame *interpreter.ExecutionFrame
	var mustClose bool
	if f, ok := input.(*interpreter.ExecutionFrame); ok {
		frame = f
	} else {
		frame, err = p.newExecutionFrame(input)
		if err != nil {
			return nil, nil, err
		}
		mustClose = true
	}
	defer func() {
		det = captureCostTracker(det, frame)
		if r := recover(); r != nil {
			switch t := r.(type) {
			case interpreter.EvalCancelledError:
				err = t
			case cost.LimitExceededError:
				err = interpreter.EvalCancelledError{
					Cause:   interpreter.CostLimitExceeded,
					Message: t.Message,
				}
			default:
				err = fmt.Errorf("internal error: %v", r)
			}
		}
		if mustClose {
			frame.Close()
		}
	}()

	if p.observable != nil {
		det = &EvalDetails{}
		out = p.observable.ObserveExec(frame, func(observed any) {
			switch o := observed.(type) {
			case interpreter.EvalState:
				det.state = o
			case *types.MemoryTracker:
				det.memTracker = o
			}
		})
	} else {
		out = p.interpretable.Exec(frame)
	}

	if types.IsError(out) {
		err = out.(*types.Err)
	}
	return
}

// ContextEval implements the Program interface.
func (p *prog) ContextEval(ctx context.Context, input any) (ref.Val, *EvalDetails, error) {
	if p == nil {
		return nil, nil, errors.New("program is nil")
	}
	if ctx == nil {
		return nil, nil, errors.New("context can not be nil")
	}
	frame, err := p.newExecutionFrame(input)
	if err != nil {
		return nil, nil, err
	}
	defer frame.Close()
	frame.SetContext(ctx, p.interruptCheckFrequency)
	out, det, errEval := p.Eval(frame)
	if errEval != nil && errors.Is(errEval, interpreter.InterruptError{}) {
		return out, det, fmt.Errorf("%w: %w", errEval, context.Cause(ctx))
	}
	return out, det, errEval
}

func (p *prog) newExecutionFrame(input any) (*interpreter.ExecutionFrame, error) {
	frame, err := interpreter.NewExecutionFrame(input)
	if err != nil {
		return nil, err
	}
	if p.defaultVars != nil {
		if err := frame.SetDefaultVars(p.defaultVars); err != nil {
			frame.Close()
			return nil, err
		}
	}
	return frame, nil
}

func (p *prog) newAsyncFrame(ctx context.Context, input any) (*interpreter.ExecutionFrame, error) {
	frame, err := p.newExecutionFrame(input)
	if err != nil {
		return nil, err
	}
	if err := frame.SetContext(ctx, p.interruptCheckFrequency); err != nil {
		frame.Close()
		return nil, err
	}
	frame.SetAsyncObserver(p.asyncObserver)
	frame.SetAsyncMaxConcurrency(resolveAsyncMaxConcurrency(p.asyncMaxConcurrency))
	return frame, nil
}

const defaultAsyncMaxConcurrency = 100

func resolveAsyncMaxConcurrency(configured int) int {
	if configured == 0 {
		return defaultAsyncMaxConcurrency
	}
	return configured
}

func (p *prog) resolveCompletionBufferSize() int {
	if p.asyncCompletionBufferSize > 0 {
		return p.asyncCompletionBufferSize
	}
	limit := resolveAsyncMaxConcurrency(p.asyncMaxConcurrency)
	if limit < 0 {
		return defaultAsyncMaxConcurrency
	}
	return limit
}

// ConcurrentEval implements the Program interface.
func (p *prog) ConcurrentEval(ctx context.Context, input any) <-chan EvalResult {
	resCh := make(chan EvalResult, 1)
	if p == nil {
		resCh <- EvalResult{Err: errors.New("program is nil")}
		close(resCh)
		return resCh
	}
	if ctx == nil {
		resCh <- EvalResult{Err: errors.New("context can not be nil")}
		close(resCh)
		return resCh
	}

	go func() {
		defer close(resCh)
		frame, err := p.newAsyncFrame(ctx, input)
		if err != nil {
			resCh <- EvalResult{Err: err}
			return
		}
		defer frame.Close()

		var det *EvalDetails
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			det = captureCostTracker(det, frame)
			switch t := r.(type) {
			case interpreter.EvalCancelledError:
				resCh <- EvalResult{EvalDetails: det, Err: t}
			case cost.LimitExceededError:
				resCh <- EvalResult{
					EvalDetails: det,
					Err: interpreter.EvalCancelledError{
						Cause:   interpreter.CostLimitExceeded,
						Message: t.Message,
					},
				}
			default:
				resCh <- EvalResult{EvalDetails: det, Err: fmt.Errorf("internal error: %v", r)}
			}
		}()

		completions := make(chan int64, p.resolveCompletionBufferSize())
		frame.SetCompletions(completions)

		for {
			var out ref.Val

			if p.observable != nil {
				det = &EvalDetails{}
				out = p.observable.ObserveExec(frame, func(observed any) {
					switch o := observed.(type) {
					case interpreter.EvalState:
						det.state = o
					case *types.MemoryTracker:
						det.memTracker = o
					}
				})
			} else {
				out = p.interpretable.Exec(frame)
			}
			det = captureCostTracker(det, frame)

			if types.IsError(out) {
				var err error = out.(*types.Err)
				if errors.Is(err, interpreter.InterruptError{}) {
					err = fmt.Errorf("%w: %w", err, context.Cause(ctx))
				}
				resCh <- EvalResult{Val: out, EvalDetails: det, Err: err}
				return
			}

			unk, isUnknown := out.(*types.Unknown)
			if !isUnknown || !unk.HasUnknownFunction() {
				resCh <- EvalResult{Val: out, EvalDetails: det, Err: nil}
				return
			}

			frame.DispatchPendingAsyncCalls(unk.IDs())

			var batch []async.Call
			select {
			case id := <-completions:
				if call := frame.AsyncCall(id); call != nil {
					batch = append(batch, call)
				}
			case <-ctx.Done():
				resCh <- EvalResult{Val: out, EvalDetails: det, Err: ctx.Err()}
				return
			}

			var timer *time.Timer
			reevaluate := false
			for !reevaluate {
				active := frame.ActiveAsyncCalls()
				action := p.drainStrategy.NextAction(batch, active)
				if action.Reevaluate {
					break
				}

				var timeoutCh <-chan time.Time
				if action.WaitDuration > 0 {
					if timer == nil {
						timer = time.NewTimer(action.WaitDuration)
					} else {
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						timer.Reset(action.WaitDuration)
					}
					timeoutCh = timer.C
				}

				select {
				case id := <-completions:
					if call := frame.AsyncCall(id); call != nil {
						batch = append(batch, call)
					}
				case <-timeoutCh:
					reevaluate = true
				case <-ctx.Done():
					if timer != nil {
						timer.Stop()
					}
					resCh <- EvalResult{Val: out, EvalDetails: det, Err: ctx.Err()}
					return
				}
			}
			if timer != nil {
				timer.Stop()
			}
		}
	}()

	return resCh
}

func buildOptimizePlanner(p *prog) func(ast.Expr) []interpreter.PlannerOption {
	return func(root ast.Expr) []interpreter.PlannerOption {
		addOptimize, addRegex := scanOptTargets(root)
		var opts []interpreter.PlannerOption
		if addOptimize {
			opts = append(opts, interpreter.Optimize())
		}
		if addRegex {
			p.regexOptimizations = append(p.regexOptimizations, interpreter.MatchesRegexOptimization)
		}
		return opts
	}
}

func buildCostObserver(p *prog) (interpreter.PlannerOption, error) {
	trackerOpts := p.Env.costModel
	costOptCount := len(trackerOpts) + len(p.costOptions) + 1
	if p.costLimit != nil {
		costOptCount++
	}
	costOpts := make([]cost.TrackerOption, 0, costOptCount)
	costOpts = append(costOpts, cost.TrackerLimitExceededHandler(func() {
		panic(interpreter.EvalCancelledError{
			Cause:   interpreter.CostLimitExceeded,
			Message: "operation cancelled: actual cost limit exceeded",
		})
	}))
	costOpts = append(costOpts, trackerOpts...)
	costOpts = append(costOpts, p.costOptions...)
	if p.costLimit != nil {
		costOpts = append(costOpts, cost.TrackerLimit(*p.costLimit))
	}
	tracker, err := cost.NewTracker(p.callCostEstimator, costOpts...)
	if err != nil {
		return nil, fmt.Errorf("construct cost tracker: %w", err)
	}
	trackerFactory := func() (*cost.Tracker, error) {
		return tracker.Clone()
	}
	return interpreter.CostObserver(interpreter.CostTrackerFactory(trackerFactory)), nil
}

func buildMemObserver(p *prog) interpreter.PlannerOption {
	memOptCount := len(p.memoryOptions)
	if p.memoryLimit != nil {
		memOptCount++
	}
	memOpts := make([]types.MemoryTrackerOption, 0, memOptCount)
	memOpts = append(memOpts, p.memoryOptions...)
	if p.memoryLimit != nil {
		memOpts = append(memOpts, types.MemoryTrackerLimit(*p.memoryLimit))
	}
	memTrackerFactory := func() (*types.MemoryTracker, error) {
		return types.NewMemoryTracker(memOpts...), nil
	}
	return interpreter.MemoryObserver(interpreter.MemoryTrackerFactory(memTrackerFactory))
}
