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

package async

import (
	"context"
	"time"

	"errors"

	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
)

// Call describes a pending or completed asynchronous function call.
// This interface exposes a safe, read-only view of the internal interpreter state.
type Call = interpreter.AsyncCall

// Observer provides callbacks for monitoring the lifecycle of asynchronous function calls.
type Observer = interpreter.AsyncObserver

// DrainAction dictates what ConcurrentEval should do after inspecting completions.
type DrainAction struct {
	// Reevaluate indicates that the AST should be re-evaluated immediately.
	// If true, WaitDuration is ignored.
	Reevaluate bool
	// WaitDuration indicates how long the evaluator should wait for additional
	// completions before deciding to re-evaluate. A duration of 0 means wait
	// indefinitely (block on the next completion).
	WaitDuration time.Duration
}

// DrainStrategy controls when ConcurrentEval re-evaluates after async completions.
//
// The evaluator consults the strategy each time a completion is received.
type DrainStrategy interface {
	// NextAction evaluates the current state of asynchronous evaluation and
	// determines the next step.
	//
	// - completed: The set of completions accumulated in the current batch.
	// - pending: The number of async calls currently launched but unresolved.
	NextAction(completed []Call, pending int) DrainAction
}

// DrainNone returns a strategy that re-evaluates after every single completion.
// This is the default strategy.
func DrainNone() DrainStrategy {
	return drainNone{}
}

type drainNone struct{}

func (drainNone) NextAction(completed []Call, pending int) DrainAction {
	return DrainAction{Reevaluate: pending == 0 || len(completed) > 0}
}

// DrainReady returns a strategy that waits for a short duration after the first
// completion to batch any other functions that complete at roughly the same time.
func DrainReady(debounce time.Duration) DrainStrategy {
	return drainReady{debounce: debounce}
}

type drainReady struct {
	debounce time.Duration
}

func (d drainReady) NextAction(completed []Call, pending int) DrainAction {
	if pending == 0 {
		return DrainAction{Reevaluate: true} // Nothing left to wait for
	}
	if len(completed) == 0 {
		return DrainAction{Reevaluate: false, WaitDuration: 0} // Wait indefinitely for first
	}
	return DrainAction{Reevaluate: false, WaitDuration: d.debounce} // Wait for debounce period
}

// DrainAll returns a strategy that waits for all currently pending calls to
// complete before re-evaluating.
//
// Note: This strategy is optimal for independent async calls, but will over-wait
// if some calls depend on the results of others.
func DrainAll() DrainStrategy {
	return drainAll{}
}

type drainAll struct{}

func (drainAll) NextAction(completed []Call, pending int) DrainAction {
	return DrainAction{Reevaluate: pending == 0}
}

// TimeoutBinding wraps a BlockingAsyncOp with a per-call timeout.
func TimeoutBinding(fn functions.BlockingAsyncOp, timeout time.Duration) decls.OverloadOpt {
	return decls.AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		tCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return fn(tCtx, args...)
	})
}

// RetryOption configures the behavior of RetryBinding.
type RetryOption func(*retryConfig)

type retryConfig struct {
	maxAttempts int
	backoff     time.Duration
}

// RetryAttempts sets the maximum number of attempts (including the first one).
func RetryAttempts(attempts int) RetryOption {
	return func(c *retryConfig) {
		c.maxAttempts = attempts
	}
}

// RetryBackoff sets the fixed backoff duration between attempts.
func RetryBackoff(backoff time.Duration) RetryOption {
	return func(c *retryConfig) {
		c.backoff = backoff
	}
}

// RetryableError is an interface that errors can implement to signal whether they are retryable.
type RetryableError interface {
	error
	IsRetryable() bool
}

// RetryBinding wraps a BlockingAsyncOp with a retry policy.
// It will retry the operation if it returns a types.Err that wraps a RetryableError returning true for IsRetryable.
func RetryBinding(fn functions.BlockingAsyncOp, opts ...RetryOption) decls.OverloadOpt {
	config := &retryConfig{
		maxAttempts: 3,
		backoff:     100 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(config)
	}

	return decls.AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		var lastErr ref.Val
		for i := 0; i < config.maxAttempts; i++ {
			if i > 0 {
				select {
				case <-time.After(config.backoff):
				case <-ctx.Done():
					return types.NewErr("operation cancelled during retry: %v", ctx.Err())
				}
			}

			res := fn(ctx, args...)
			if !types.IsError(res) {
				return res
			}

			err := res.(*types.Err)
			lastErr = res

			if !isRetryable(err) {
				return res
			}
		}
		return lastErr
	})
}

func isRetryable(err *types.Err) bool {
	if err == nil {
		return false
	}
	var re RetryableError
	if errors.As(err, &re) {
		return re.IsRetryable()
	}
	return false
}

// Limiter provides concurrency control across one or more async functions.
type Limiter struct {
	sem chan struct{}
}

// NewLimiter creates a new limiter with the specified maximum concurrency.
func NewLimiter(maxConcurrency int) *Limiter {
	return &Limiter{sem: make(chan struct{}, maxConcurrency)}
}

// Limit wraps a BlockingAsyncOp to enforce the limiter's concurrency limit.
func (l *Limiter) Limit(fn functions.BlockingAsyncOp) functions.BlockingAsyncOp {
	return func(ctx context.Context, args ...ref.Val) ref.Val {
		select {
		case l.sem <- struct{}{}:
			defer func() { <-l.sem }()
			return fn(ctx, args...)
		case <-ctx.Done():
			return types.NewErr("concurrency limit wait cancelled: %v", ctx.Err())
		}
	}
}
