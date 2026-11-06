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

package cel_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/ext"
)

// evalTraceGolden locks down the EvalTrace JSON format. The comprehension has no "var" since `k`
// is bound to the map key rather than to the iterated element.
const evalTraceGolden = `{
  "version": 1,
  "expression": "items.all(k, items[k] \u003e 0)",
  "result": "false",
  "exprs": [
    {"id": 1, "expr": "items"},
    {"id": 5, "expr": "items[k]"},
    {"id": 7, "expr": "items[k] \u003e 0"},
    {"id": 8, "expr": "0"},
    {"id": 9, "expr": "true", "internal": true},
    {"id": 10, "expr": "@result", "internal": true},
    {"id": 11, "expr": "@not_strictly_false(@result)", "internal": true},
    {"id": 12, "expr": "@result", "internal": true},
    {"id": 13, "expr": "@result \u0026\u0026 items[k] \u003e 0", "internal": true},
    {"id": 14, "expr": "@result", "internal": true},
    {"id": 15, "expr": "items.all(k, items[k] \u003e 0)", "range": "items"}
  ],
  "records": [
    {"id": 1, "value": "{a: 0}"},
    {"id": 15, "key": "\"a\""},
    {"id": 9, "value": "true"},
    {"id": 10, "parent": 2, "value": "true"},
    {"id": 11, "parent": 2, "value": "true"},
    {"id": 12, "parent": 2, "value": "true"},
    {"id": 5, "parent": 2, "value": "0"},
    {"id": 8, "parent": 2, "value": "0"},
    {"id": 7, "parent": 2, "value": "false"},
    {"id": 13, "parent": 2, "value": "false"},
    {"id": 14, "value": "false"},
    {"id": 15, "value": "false"}
  ]
}`

func TestEvalTraceGolden(t *testing.T) {
	env, err := cel.NewEnv(cel.Variable("items", cel.MapType(cel.StringType, cel.IntType)))
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, iss := env.Compile(`items.all(k, items[k] > 0)`)
	if iss.Err() != nil {
		t.Fatalf("Compile() failed: %v", iss.Err())
	}
	prg, err := env.Program(ast, cel.EvalOptions(cel.OptTraceIterations))
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	_, det, err := prg.Eval(map[string]any{"items": map[string]int64{"a": 0}})
	if err != nil {
		t.Fatalf("Eval() failed: %v", err)
	}
	exp, err := cel.ExplainEval(ast, det)
	if err != nil {
		t.Fatalf("ExplainEval() failed: %v", err)
	}
	got, err := json.Marshal(exp.Trace())
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, []byte(evalTraceGolden)); err != nil {
		t.Fatalf("json.Compact() failed: %v", err)
	}
	if string(got) != want.String() {
		t.Errorf("json.Marshal(Trace()) got:\n%s\nwanted:\n%s", got, want.String())
	}
	if tr := exp.Trace(); tr.Result != types.False || tr.Records[1].Key != types.String("a") {
		t.Errorf("Trace() got result %v and key %v, wanted CEL values", tr.Result, tr.Records[1].Key)
	}
}

// traceFor evaluates the expression over the object and returns its trace.
func traceFor(t *testing.T, expr string, object map[string]any) *cel.EvalTrace {
	t.Helper()
	env, err := cel.NewEnv(cel.Variable("object", cel.DynType), ext.Bindings())
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, iss := env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("Compile(%q) failed: %v", expr, iss.Err())
	}
	prg, err := env.Program(ast, cel.EvalOptions(cel.OptExhaustiveEval, cel.OptTraceIterations))
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	_, det, _ := prg.Eval(map[string]any{"object": object})
	exp, err := cel.ExplainEval(ast, det)
	if err != nil {
		t.Fatalf("ExplainEval() failed: %v", err)
	}
	return exp.Trace()
}

func TestCompareEvalTraces(t *testing.T) {
	expr := `object.spec.containers.all(c, c.securityContext.privileged == false)`
	before := explainPod()
	after := explainPod()
	// Fix the privileged sidecar.
	after["spec"].(map[string]any)["containers"].([]any)[1].(map[string]any)["securityContext"] =
		map[string]any{"privileged": false}

	diffs, err := cel.CompareEvalTraces(traceFor(t, expr, before), traceFor(t, expr, after))
	if err != nil {
		t.Fatalf("CompareEvalTraces() failed: %v", err)
	}
	var got []string
	for _, d := range diffs {
		// Skip values of whole objects to keep the expectation readable.
		if !strings.HasPrefix(d.Expr, "object.") {
			got = append(got, d.String())
		}
	}
	want := []string{
		`object.spec.containers[1]: c.securityContext: {privileged: true} -> {privileged: false}`,
		`object.spec.containers[1]: c.securityContext.privileged: true -> false`,
		`object.spec.containers[1]: c.securityContext.privileged == false: false -> true`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("CompareEvalTraces() got:\n%s\nwanted:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	resultChanged := false
	for _, d := range diffs {
		if strings.HasPrefix(d.Expr, "object.spec.containers.all(") {
			resultChanged = d.Before == types.False && d.After == types.True
		}
	}
	if !resultChanged {
		t.Errorf("CompareEvalTraces() did not report the result changing from false to true: %v", diffs)
	}
}

func TestCompareEvalTracesErrorValues(t *testing.T) {
	// initContainers[0] has no securityContext, so its predicate is an error in both evaluations.
	// Errors with the same message are the same value, so only the fixed container differs.
	expr := `object.spec.initContainers.all(c, c.securityContext.privileged == false)`
	after := explainPod()
	after["spec"].(map[string]any)["initContainers"].([]any)[1].(map[string]any)["securityContext"] =
		map[string]any{"privileged": false}
	diffs, err := cel.CompareEvalTraces(traceFor(t, expr, explainPod()), traceFor(t, expr, after))
	if err != nil {
		t.Fatalf("CompareEvalTraces() failed: %v", err)
	}
	for _, d := range diffs {
		if strings.Contains(d.Path, "[0]") {
			t.Errorf("CompareEvalTraces() reported the unchanged error value: %v", d)
		}
	}
	if len(diffs) == 0 {
		t.Error("CompareEvalTraces() reported no differences, wanted the fixed container")
	}
}

func TestCompareEvalTracesMapOrder(t *testing.T) {
	// Go map iteration order varies between evaluations, but iterations are matched by key.
	labels := map[string]any{}
	for i := range 32 {
		labels[fmt.Sprintf("label-%02d", i)] = fmt.Sprintf("value-%d", i)
	}
	object := map[string]any{"metadata": map[string]any{"labels": labels}}
	expr := `object.metadata.labels.all(k, object.metadata.labels[k].startsWith('value-'))`
	for range 5 {
		diffs, err := cel.CompareEvalTraces(traceFor(t, expr, object), traceFor(t, expr, object))
		if err != nil {
			t.Fatalf("CompareEvalTraces() failed: %v", err)
		}
		if len(diffs) != 0 {
			t.Fatalf("CompareEvalTraces() of identical evaluations got %v, wanted none", diffs)
		}
	}
}

func TestCompareEvalTracesErrors(t *testing.T) {
	a := traceFor(t, `object.spec.containers.size() == 2`, explainPod())
	b := traceFor(t, `object.spec.containers.size() == 3`, explainPod())
	if _, err := cel.CompareEvalTraces(a, b); err == nil {
		t.Error("CompareEvalTraces() of different expressions succeeded")
	}
	if _, err := cel.CompareEvalTraces(nil, a); err == nil {
		t.Error("CompareEvalTraces(nil) succeeded")
	}
	future := *a
	future.Version = cel.EvalTraceVersion + 1
	if _, err := cel.CompareEvalTraces(&future, a); err == nil {
		t.Error("CompareEvalTraces() of an unsupported version succeeded")
	}
	// Parents must be earlier iteration records.
	malformed := *a
	malformed.Records = []cel.TraceEntry{{ID: 1, Parent: 7, Value: types.True}}
	if _, err := cel.CompareEvalTraces(&malformed, a); err == nil {
		t.Error("CompareEvalTraces() with a parent beyond the record succeeded")
	}
	malformed.Records = []cel.TraceEntry{{ID: 1, Value: types.True}, {ID: 2, Parent: 1, Value: types.True}}
	if _, err := cel.CompareEvalTraces(a, &malformed); err == nil {
		t.Error("CompareEvalTraces() with a value record as a parent succeeded")
	}
	// A value present in only one trace is reported as absent from the other.
	removed := *a
	removed.Records = nil
	diffs, err := cel.CompareEvalTraces(a, &removed)
	if err != nil || len(diffs) == 0 || diffs[0].After != nil {
		t.Fatalf("CompareEvalTraces() with removed values got %v, %v", diffs, err)
	}
	if got := diffs[0].String(); !strings.HasSuffix(got, "-> <absent>") {
		t.Errorf("TraceDifference.String() got %q, wanted an absent value", got)
	}
}
