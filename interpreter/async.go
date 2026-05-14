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
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Async extension function support.
//
// CEL supports `types.Unknown` as a first-class value, and concurrent (async) function execution
// in CEL invokes a stub function which checks for the presence of an existing result which matches
// the function call and call arguments, or which records the 'unexecuted' function and call arguments
// for concurrent execution in a later phase if the result is `types.Unknown` and indicates the
// expression ids of the functions necessary to advance the execution.
//
// This call pattern is repeated iteratively until there are either no more functions to call or no
// progress is made toward resolving the unknowns.

type evalAsyncFunc struct {
	id       int64
	function string
	overload string
	args     []InterpretableV2
	impl     functions.AsyncOp
}

func (fn *evalAsyncFunc) ID() int64 {
	return fn.id
}

func (fn *evalAsyncFunc) Function() string {
	return fn.function
}

func (fn *evalAsyncFunc) OverloadID() string {
	return fn.overload
}

func (fn *evalAsyncFunc) Args() []InterpretableV2 {
	return fn.args
}

func (fn *evalAsyncFunc) Eval(vars Activation) ref.Val {
	return fn.Exec(AsFrame(vars))
}

func (fn *evalAsyncFunc) Exec(frame *ExecutionFrame) ref.Val {
	argVals := make([]ref.Val, len(fn.args))
	// Early return if any argument to the function is unknown or error.
	for i, arg := range fn.args {
		argVals[i] = arg.Exec(frame)
		if types.IsUnknownOrError(argVals[i]) {
			return argVals[i]
		}
	}
	result := frame.ComputeResult(fn.ID(), fn.Function(), fn.OverloadID(), fn.impl, argVals)
	return types.LabelErrNode(fn.id, result)
}
