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
	"testing"

	"github.com/google/cel-go/common/containers"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestFolderHierarchy(t *testing.T) {
	// This test exercises folder.Parent and folder.Unwrap which are used during comprehensions.
	parentActivation, err := NewActivation(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("NewActivation() failed: %v", err)
	}
	parentFrame := NewExecutionFrame(parentActivation)

	// Create a folder (internal to comprehensions)
	// We need to simulate the state where a folder is active.
	accuActivation, err := NewActivation(map[string]any{"accu": 0})
	if err != nil {
		t.Fatalf("NewActivation() failed: %v", err)
	}
	fld := &folder{
		evalFold: &evalFold{
			accuVar: "accu",
		},
		frame: parentFrame.push(accuActivation),
	}

	if fld.Parent() != parentFrame {
		t.Errorf("folder.Parent() returned %v, wanted %v", fld.Parent(), parentFrame)
	}

	if fld.Unwrap() != parentFrame {
		t.Errorf("folder.Unwrap() returned %v, wanted %v", fld.Unwrap(), parentFrame)
	}
}

func TestEvalWatchConstructor(t *testing.T) {
	// Exercise evalWatchConstructor.InitVals and Type
	list := &evalList{
		elems: []InterpretableV2{NewConstValue(1, types.Int(1))},
	}

	observer := func(vars Activation, id int64, progStep any, val ref.Val) {}

	watcher := &evalWatchConstructor{
		constructor: list,
		observer:    observer,
	}

	if len(watcher.InitVals()) != 1 {
		t.Errorf("watcher.InitVals() returned length %d, wanted 1", len(watcher.InitVals()))
	}

	if watcher.Type() != types.ListType {
		t.Errorf("watcher.Type() returned %v, wanted %v", watcher.Type(), types.ListType)
	}
}

func TestInterruptError(t *testing.T) {
	ie := InterruptError{}
	if !ie.Is(errors.New("operation interrupted")) {
		t.Error("InterruptError.Is() returned false for same error message")
	}
	if ie.Is(errors.New("other")) {
		t.Error("InterruptError.Is() returned true for different error message")
	}
}

func TestTestOnlyQualifier(t *testing.T) {
	reg, _ := types.NewRegistry()
	fac := NewAttributeFactory(containers.DefaultContainer, reg, reg)
	activation, _ := NewActivation(map[string]any{"a": map[string]any{"b": 1}})

	attr := fac.AbsoluteAttribute(1, "a")
	qual, _ := fac.NewQualifier(nil, 2, "b", false)
	attr.AddQualifier(qual)

	testOnly := &evalTestOnly{
		id: 3,
		InterpretableAttribute: &evalAttr{
			adapter: reg,
			attr:    attr,
		},
	}

	q, _ := fac.NewQualifier(nil, 4, "b", false)
	toqAttr, err := testOnly.AddQualifier(q)
	if err != nil {
		t.Fatalf("AddQualifier() failed: %v", err)
	}

	// toqAttr is a new attribute with testOnlyQualifier
	quals := toqAttr.(NamespacedAttribute).Qualifiers()
	toq := quals[len(quals)-1]
	res, found, err := toq.QualifyIfPresent(activation, map[string]any{"b": 1}, true)
	if err != nil {
		t.Fatalf("QualifyIfPresent() failed: %v", err)
	}
	if !found || res != 1 {
		t.Errorf("QualifyIfPresent() returned (%v, %v), wanted (1, true)", res, found)
	}

	// QualifierValueEquals
	if !toq.(qualifierValueEquator).QualifierValueEquals("b") {
		t.Error("QualifierValueEquals() returned false, wanted true")
	}
}
