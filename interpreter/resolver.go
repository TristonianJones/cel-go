// Copyright 2019 Google LLC
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
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	anypb "github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

// Resolver interface defines methods for resolving both absolute and relative attributes.
//
// One of the most expensive operations in CEL is object accessor reflection. When an Attribute
// appears in the CEL expression, in most cases, the intent of the user is to select a leaf node
// of a complex object which is of a simple primitive type. In these scenarios, the Resolver is
// ideally situated to look at the Attribute path and find the most optimal way to retrieve the
// data.
//
// For applications which use proto-based inputs or custom types with relatively static
// environments are encouraged to hand-roll Resolver implementations suited to their strongly
// typed objects.
type Resolver interface {
	// Resolve finds a top-level Attribute from the given Activation, returning the value
	// if present.
	//
	// When the resolver cannot find the Attribute's variable in the activation it must return
	// nil, false since the resolution of attributes within unchecked expressions may be many
	// alternative Attribute representations, and disambiguation must be error free.
	//
	// When the resolver finds an Attribute's variable, but the Attribute's path is not defined,
	// the resolve must return types.Err, true where the error corresponds to either 'no_such_key',
	// or 'no_such_field'.
	Resolve(Activation, Attribute) (interface{}, bool)

	// ResolveRelative finds a relative Attribute from the input object and Activation.
	//
	// If the Attribute cannot be found, the return value must be a types.Err value.
	//
	// Unlike the Resolve call which always operates on attributes of top-level variables, the
	// relative call always operates on a function result.
	ResolveRelative(interface{}, Activation, Attribute) interface{}
}

// NewResolver creates a Resolver from a type adapter.
func NewResolver(adapter ref.TypeAdapter) Resolver {
	return &defaultResolver{adapter: adapter}
}

// NewUnknownResolver creates a Resolver capable of inspecting a PartialActivation for the presence
// of unknown values.
func NewUnknownResolver(resolver Resolver) Resolver {
	return &unknownResolver{
		Resolver: resolver,
	}
}

// ResolveStatus indicates the possible resolution outcomes for an Attribute.
type ResolveStatus int

const (
	// FoundAttribute indicates that a top-level variable was found and that its path resolved
	// to a value.
	FoundAttribute ResolveStatus = 1 << iota

	// NoSuchVariable indicates that the top-level variable was not provided in the Activation.
	NoSuchVariable ResolveStatus = 1 << iota

	// NoSuchAttribute indicates that a top-level variable was found, but the referenced Attribute
	// could not be resolved.
	NoSuchAttribute ResolveStatus = 1 << iota

	// UnknownAttribute indicates that the Attribute path matched an unknown Attribute path.
	UnknownAttribute ResolveStatus = 1 << iota
)

// ResolveListener receives an Attribute and the status of the Resolve call for each Attribute
// provided to the Resolve call during the course of expression evaluation.
type ResolveListener func(Attribute, ResolveStatus)

// NewListeningResolver creates a Resolver that intercepts calls to Resolve and reports their
// resolution status.
func NewListeningResolver(resolver Resolver, listener ResolveListener) Resolver {
	return &listeningResolver{
		Resolver: resolver,
		listener: listener,
	}
}

// defaultResolver handles the resolution of attributes within simple Go native types.
type defaultResolver struct {
	adapter ref.TypeAdapter
}

// Resolve implements the Resolver interface method.
func (res *defaultResolver) Resolve(vars Activation, attr Attribute) (interface{}, bool) {
	varName := attr.Variable().Name()
	attrPath := attr.Path()
	obj, found := vars.Find(varName)
	if !found {
		return nil, false
	}
	for _, elem := range attrPath {
		obj = res.getElem(obj, elem.ToValue(vars))
	}
	return obj, true
}

// ResolveRelative implements the Resolver interface method.
func (res *defaultResolver) ResolveRelative(
	obj interface{},
	vars Activation,
	attr Attribute) interface{} {
	for _, elem := range attr.Path() {
		obj = res.getElem(obj, elem.ToValue(vars))
	}
	return obj
}

// getElem attempts to get the next 'elem' from the target 'obj' either by using native golang
// method appropriate for the 'obj' type or by converting the obj to a ref.Val and using the CEL
// functions.
func (res *defaultResolver) getElem(obj interface{}, elem ref.Val) interface{} {
	switch o := obj.(type) {
	case map[string]interface{}:
		key, ok := elem.(types.String)
		if !ok {
			return types.ValOrErr(elem, "no such overload")
		}
		v, found := o[string(key)]
		if !found {
			return types.NewErr("no such key")
		}
		return v
	case map[string]string:
		return res.getMapStrVal(o, elem)
	case map[string]float32:
		return res.getMapFloat32Val(o, elem)
	case map[string]float64:
		return res.getMapFloat64Val(o, elem)
	case map[string]int:
		return res.getMapIntVal(o, elem)
	case map[string]int32:
		return res.getMapInt32Val(o, elem)
	case map[string]int64:
		return res.getMapInt64Val(o, elem)
	case map[string]bool:
		return res.getMapBoolVal(o, elem)
	case []interface{}:
		return res.getListIFaceVal(o, elem)
	case []string:
		return res.getListStrVal(o, elem)
	case []float32:
		return res.getListFloat32Val(o, elem)
	case []float64:
		return res.getListFloat64Val(o, elem)
	case []int:
		return res.getListIntVal(o, elem)
	case []int32:
		return res.getListInt32Val(o, elem)
	case []int64:
		return res.getListInt64Val(o, elem)
	case proto.Message:
		return res.getProtoField(o, elem)
	case traits.Indexer:
		return o.Get(elem)
	case *types.Err, types.Unknown:
		// It is safe to continue evaluation after an error or unknown, since the first value of
		// either type will be preserved throughout the evaluation of the attribute. This may
		// appear sub-optimal, but these cases are the exception and not the rule, and checking
		// whether the value is an error or unknown is an expense that should not be incurred in
		// the happy path.
		return o
	case ref.Val:
		return types.ValOrErr(o, "no such overload")
	default:
		objType := reflect.TypeOf(o)
		objKind := objType.Kind()
		if objKind == reflect.Map ||
			objKind == reflect.Array ||
			objKind == reflect.Slice {
			val := res.adapter.NativeToValue(obj)
			indexer, ok := val.(traits.Indexer)
			if ok {
				return indexer.Get(elem)
			}
			return types.ValOrErr(val, "no such overload")
		}
		return types.NewErr("no such overload")
	}
}

func (res *defaultResolver) getMapStrVal(m map[string]string, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		return types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapFloat32Val(m map[string]float32, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapFloat64Val(m map[string]float64, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapIntVal(m map[string]int, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapInt32Val(m map[string]int32, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapInt64Val(m map[string]int64, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getMapBoolVal(m map[string]bool, k ref.Val) interface{} {
	key, ok := k.(types.String)
	if !ok {
		types.ValOrErr(k, "no such overload")
	}
	v, found := m[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return v
}

func (res *defaultResolver) getListIFaceVal(l []interface{}, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListStrVal(l []string, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListFloat32Val(l []float32, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListFloat64Val(l []float64, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListIntVal(l []int, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListInt32Val(l []int32, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getListInt64Val(l []int64, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	if index < 0 || index >= len(l) {
		return types.ValOrErr(idx, "index out of range")
	}
	return l[index]
}

func (res *defaultResolver) getStructField(m *structpb.Struct, k ref.Val) interface{} {
	fields := m.GetFields()
	key, ok := k.(types.String)
	if !ok {
		return types.ValOrErr(k, "no such overload")
	}
	value, found := fields[string(key)]
	if !found {
		return types.NewErr("no such key")
	}
	return maybeUnwrapValue(value)
}

func (res *defaultResolver) getListValueElem(l *structpb.ListValue, i ref.Val) interface{} {
	idx, ok := i.(types.Int)
	if !ok {
		return types.ValOrErr(i, "no such overload")
	}
	index := int(idx)
	elems := l.GetValues()
	if index < 0 || index >= len(elems) {
		return types.NewErr("index out of range")
	}
	return maybeUnwrapValue(elems[index])
}

func (res *defaultResolver) getProtoField(obj proto.Message, elem ref.Val) interface{} {
	switch o := obj.(type) {
	case *anypb.Any:
		if o == nil {
			return types.NewErr("unsupported type conversion: '%T'", obj)
		}
		unpackedAny := ptypes.DynamicAny{}
		if ptypes.UnmarshalAny(o, &unpackedAny) != nil {
			return types.NewErr("unknown type: '%s'", o.GetTypeUrl())
		}
		return res.getProtoField(unpackedAny.Message, elem)
	case *structpb.Value:
		if o == nil {
			return types.NewErr("no such overload")
		}
		switch o.Kind.(type) {
		case *structpb.Value_StructValue:
			return res.getProtoField(o.GetStructValue(), elem)
		case *structpb.Value_ListValue:
			return res.getProtoField(o.GetListValue(), elem)
		default:
			return types.NewErr("no such overload")
		}
	case *structpb.Struct:
		return res.getStructField(o, elem)
	case *structpb.ListValue:
		return res.getListValueElem(o, elem)
	default:
		pb := res.adapter.NativeToValue(o)
		indexer, ok := pb.(traits.Indexer)
		if !ok {
			return types.ValOrErr(pb, "no such overload")
		}
		return indexer.Get(elem)
	}
}

// maybeUnwrapValue will unwrap bool, null, number, and string JSON values to their go-native
// representations.
func maybeUnwrapValue(v *structpb.Value) interface{} {
	switch v.Kind.(type) {
	case *structpb.Value_BoolValue:
		return v.GetBoolValue()
	case *structpb.Value_NullValue:
		return types.NullValue
	case *structpb.Value_NumberValue:
		return v.GetNumberValue()
	case *structpb.Value_StringValue:
		return v.GetStringValue()
	default:
		return v
	}
}

// listeningResolver acts as an interceptor that reports when Attribute resolution was attempted.
type listeningResolver struct {
	Resolver
	listener ResolveListener
}

// Resolve intercepts the Resolver interface method and emits resolution status for the attribute
// provided.
func (res *listeningResolver) Resolve(vars Activation, attr Attribute) (interface{}, bool) {
	v, found := res.Resolver.Resolve(vars, attr)
	// Return no such variable if the top-level variable could not be found.
	if !found {
		res.listener(attr, NoSuchVariable)
		return v, found
	}
	// Handle the unknown case.
	unk, isUnk := v.(types.Unknown)
	if isUnk {
		// Figure out the point in the attribute at which the unknown is found.
		res.listener(ancestorAttr(attr, unk[0]), UnknownAttribute)
		return v, found
	}
	// Handle the error vs. found case.
	_, isErr := v.(*types.Err)
	if isErr {
		// TODO: make errors useful by including the expression id where the Error occurs, and then
		// apply the same logic for getting the ancestor attribute for unknowns to errors.
		res.listener(attr, NoSuchAttribute)
		return v, found
	}
	res.listener(attr, FoundAttribute)
	return v, found
}

// ancestorAttr attempts to determine Attribute or the nearest ancestor that matches the given
// ancestorID.
func ancestorAttr(attr Attribute, ancestorID int64) Attribute {
	aVar := attr.Variable()
	if aVar.ID() == ancestorID {
		return &attribute{variable: aVar, path: noPathElems}
	}
	for i, elem := range attr.Path() {
		if elem.ID == ancestorID {
			return &attribute{variable: aVar, path: attr.Path()[:i+1]}
		}
	}
	return attr
}

// unknownResolver acts as an interceptor that inspects whether top-level Attribute values
// have been marked as declared but as-yet unknown.
type unknownResolver struct {
	Resolver
}

// Resolve implements the Resovler interface method and tests whether the incoming Attribute or
// any Attribute above or below it was marked as unknown.
func (res *unknownResolver) Resolve(vars Activation, attr Attribute) (interface{}, bool) {
	partial, ok := vars.(PartialActivation)
	if !ok {
		return res.Resolver.Resolve(vars, attr)
	}
	varName := attr.Variable().Name()
	candUnknowns, found := partial.FindUnknowns(varName)
	if !found {
		return res.Resolver.Resolve(vars, attr)
	}
	varPath := attr.Path()
	varPathLen := len(varPath)
	varID := attr.Variable().ID()
	for _, cand := range candUnknowns {
		isMatch := true
		candPath := cand.Path()
		candUnkID := varID
		for j, candElem := range candPath {
			if j >= varPathLen {
				break
			}
			candUnkID = varPath[j].ID
			candElemVal := candElem.ToValue(vars)
			if candElemVal == wildcard {
				continue
			}
			varElemVal := varPath[j].ToValue(vars)
			if candElemVal.Equal(varElemVal) != types.True {
				isMatch = false
				break
			}
		}
		// TODO: return the correct identifier based on the last known match pos.
		if isMatch {
			return types.Unknown{candUnkID}, true
		}
	}
	return res.Resolver.Resolve(vars, attr)
}

// wildcards in attributes indicates that any selection will match an unknown attribute.
var wildcard = types.String("*")
var noPathElems = []*PathElem{}
