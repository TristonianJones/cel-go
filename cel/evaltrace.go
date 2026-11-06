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

package cel

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"cel.dev/cel-go/common/types/ref"
)

// EvalTraceVersion is the version of the EvalTrace JSON format.
//
// The version is incremented whenever a change to the format would cause a consumer of the previous
// version to misinterpret a trace. Fields may be added without a version change.
const EvalTraceVersion = 1

// EvalTrace is a record of an evaluation recorded with OptTraceIterations.
//
// A trace is self-describing: alongside the record stream, it holds the source text of the
// expression and of every expression referenced by the stream, so it can be compared with another
// trace with CompareEvalTraces, or rendered by an external viewer without the AST.
//
// The JSON encoding is intended for storage and display. Values are rendered as human-readable
// text by the process which produced the trace, and are not decoded back into CEL values:
//
//	{
//	  "version": 1,
//	  "expression": "items.all(i, i.ok)",
//	  "result": "false",
//	  "exprs": [{"id": 4, "expr": "i.ok"}, {"id": 9, "expr": "items.all(i, i.ok)", "range": "items", "var": "i"}],
//	  "records": [
//	    {"id": 9, "key": "0"},
//	    {"id": 4, "parent": 1, "value": "false"},
//	    {"id": 9, "value": "false"}
//	  ]
//	}
//
// Records are numbered from one. A record with a key begins an iteration of the comprehension id,
// and is the parent of the records made within the iteration. Other records hold the value of id.
type EvalTrace struct {
	// Version is the EvalTraceVersion of the format.
	Version int

	// Expression is the source text of the evaluated expression, if available.
	Expression string

	// Result is the value of the expression.
	Result ref.Val

	// Exprs describes the expressions referenced by the records, ordered by id.
	Exprs []TraceExpr

	// Records is the record stream in evaluation order.
	Records []TraceEntry
}

// MarshalJSON renders the trace with human-readable values.
func (tr *EvalTrace) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version    int          `json:"version"`
		Expression string       `json:"expression,omitempty"`
		Result     string       `json:"result,omitempty"`
		Exprs      []TraceExpr  `json:"exprs"`
		Records    []TraceEntry `json:"records"`
	}{tr.Version, tr.Expression, valueText(tr.Result), tr.Exprs, tr.Records})
}

// TraceExpr describes an expression referenced by an EvalTrace.
type TraceExpr struct {
	// ID is the expression id.
	ID int64 `json:"id"`

	// Expr is the source text of the expression.
	Expr string `json:"expr"`

	// Internal indicates that the expression implements comprehension bookkeeping, such as an
	// accumulator reference, rather than an expression written by the user.
	Internal bool `json:"internal,omitempty"`

	// Range is the source text of the iteration range of a comprehension.
	Range string `json:"range,omitempty"`

	// Var is the comprehension variable bound to the element of each iteration, if any.
	Var string `json:"var,omitempty"`
}

// TraceEntry is a single entry in the record stream of an EvalTrace.
type TraceEntry struct {
	// ID is the id of the comprehension for an iteration, and of the expression for a value.
	ID int64

	// Parent is the number of the record which began the iteration within which the entry was
	// made, or zero outside of any iteration.
	Parent int

	// Key is the list index or map key of an iteration, and nil for a value.
	Key ref.Val

	// Value is the value of the expression.
	Value ref.Val
}

// MarshalJSON renders the entry with human-readable values.
func (e TraceEntry) MarshalJSON() ([]byte, error) {
	key := ""
	if e.Key != nil {
		key = renderKey(e.Key)
	}
	return json.Marshal(struct {
		ID     int64  `json:"id"`
		Parent int    `json:"parent,omitempty"`
		Key    string `json:"key,omitempty"`
		Value  string `json:"value,omitempty"`
	}{e.ID, e.Parent, key, valueText(e.Value)})
}

// valueText renders a value for the JSON form of a trace, or the empty string for no value.
func valueText(val ref.Val) string {
	if val == nil {
		return ""
	}
	return formatExplainValue(val)
}

// Trace returns the record of the evaluation in a self-describing form.
func (e *Explanation) Trace() *EvalTrace {
	tr := &EvalTrace{
		Version:    EvalTraceVersion,
		Expression: e.source,
		Result:     e.Result(),
		Exprs:      []TraceExpr{},
		Records:    make([]TraceEntry, len(e.records)),
	}
	exprs := map[int64]TraceExpr{}
	for i, r := range e.records {
		tr.Records[i] = TraceEntry(r)
		te, found := exprs[r.ID]
		if !found {
			te = TraceExpr{ID: r.ID, Expr: e.exprText(r.ID), Internal: e.internal[r.ID]}
		}
		if r.IsIteration() && !found {
			iter := e.iteration(i + 1)
			te.Range, te.Var = iter.Range, iter.elemVar
		}
		exprs[r.ID] = te
	}
	for _, te := range exprs {
		tr.Exprs = append(tr.Exprs, te)
	}
	slices.SortFunc(tr.Exprs, func(a, b TraceExpr) int { return cmp.Compare(a.ID, b.ID) })
	return tr
}

// TraceDifference describes an expression value which differs between two traces.
type TraceDifference struct {
	// Path renders the comprehension iterations in which the value was observed.
	Path string

	// ExprID is the expression id.
	ExprID int64

	// Expr is the source text of the expression.
	Expr string

	// Before and After are the values in each trace, or nil if the value was not observed.
	Before ref.Val
	After  ref.Val
}

// String renders the difference.
func (d TraceDifference) String() string {
	loc := d.Expr
	if d.Path != "" {
		loc = d.Path + ": " + loc
	}
	return fmt.Sprintf("%s: %s -> %s", loc, diffText(d.Before), diffText(d.After))
}

// diffText renders a compared value, which is nil when absent from a trace.
func diffText(val ref.Val) string {
	if val == nil {
		return "<absent>"
	}
	return formatExplainValue(val)
}

// CompareEvalTraces returns the expression values which differ between two traces of the same
// expression, such as the evaluations of a policy before and after a fix to the input.
//
// Values are matched by expression id and by the keys of the enclosing iterations, so map entries
// are matched even when maps are iterated in a different order. Comprehension bookkeeping is not
// compared. Differences are reported in the evaluation order of the after trace, followed by values
// only in the before trace.
func CompareEvalTraces(before, after *EvalTrace) ([]TraceDifference, error) {
	if before == nil || after == nil {
		return nil, errors.New("compare: nil trace")
	}
	for _, tr := range []*EvalTrace{before, after} {
		if tr.Version != EvalTraceVersion {
			return nil, fmt.Errorf("compare: unsupported trace version %d", tr.Version)
		}
	}
	if before.Expression != after.Expression {
		return nil, errors.New("compare: traces are of different expressions")
	}
	b, err := indexTrace(before)
	if err != nil {
		return nil, err
	}
	a, err := indexTrace(after)
	if err != nil {
		return nil, err
	}
	var diffs []TraceDifference
	for _, key := range a.order {
		av := a.values[key]
		if bv, found := b.values[key]; !found || !sameExplainValue(bv.value, av.value) {
			diffs = append(diffs, TraceDifference{
				Path: av.path, ExprID: key.expr, Expr: a.exprs[key.expr].Expr,
				Before: b.values[key].value, After: av.value,
			})
		}
	}
	for _, key := range b.order {
		if _, found := a.values[key]; !found {
			bv := b.values[key]
			diffs = append(diffs, TraceDifference{
				Path: bv.path, ExprID: key.expr, Expr: b.exprs[key.expr].Expr, Before: bv.value,
			})
		}
	}
	return diffs, nil
}

type traceValueKey struct {
	path string
	expr int64
}

type indexedTraceValue struct {
	path  string
	value ref.Val
}

type indexedTrace struct {
	exprs  map[int64]TraceExpr
	values map[traceValueKey]indexedTraceValue
	order  []traceValueKey
}

// indexTrace computes the last value of each expression within each iteration of a trace, keyed by
// the iteration keys of the enclosing iterations.
func indexTrace(tr *EvalTrace) (*indexedTrace, error) {
	idx := &indexedTrace{
		exprs:  map[int64]TraceExpr{},
		values: map[traceValueKey]indexedTraceValue{},
	}
	for _, e := range tr.Exprs {
		idx.exprs[e.ID] = e
	}
	// paths holds the iteration path of each iteration record, indexed by record number, and nil
	// for value records. Parents must be earlier iteration records, or zero.
	paths := make([][]IterationRef, len(tr.Records)+1)
	paths[0] = []IterationRef{}
	for i, r := range tr.Records {
		if r.Parent < 0 || r.Parent > i || paths[r.Parent] == nil {
			return nil, fmt.Errorf("compare: malformed record %d", i+1)
		}
		if r.Key != nil {
			comp := idx.exprs[r.ID]
			paths[i+1] = append(slices.Clip(paths[r.Parent]), IterationRef{
				ComprehensionID: r.ID, Range: comp.Range, Key: r.Key, elemVar: comp.Var,
			})
			continue
		}
		if idx.exprs[r.ID].Internal || r.Value == nil {
			continue
		}
		key := traceValueKey{path: iterationKey(paths[r.Parent]), expr: r.ID}
		if _, found := idx.values[key]; !found {
			idx.order = append(idx.order, key)
		}
		idx.values[key] = indexedTraceValue{path: formatIterationPath(paths[r.Parent]), value: r.Value}
	}
	return idx, nil
}

// iterationKey identifies an iteration path by the comprehensions and keys of its iterations.
func iterationKey(path []IterationRef) string {
	var sb strings.Builder
	for _, r := range path {
		fmt.Fprintf(&sb, "/%d[%s]", r.ComprehensionID, renderKey(r.Key))
	}
	return sb.String()
}
