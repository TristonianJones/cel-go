// Copyright 2022 Google LLC
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
	"fmt"

	chkdecls "github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Kind indicates a CEL type's kind which is used to differentiate quickly between simple and complex types.
type Kind = decls.Kind

const (
	// DynKind represents a dynamic type. This kind only exists at type-check time.
	DynKind Kind = decls.DynKind

	// AnyKind represents a google.protobuf.Any type. This kind only exists at type-check time.
	AnyKind = decls.AnyKind

	// BoolKind represents a boolean type.
	BoolKind = decls.BoolKind

	// BytesKind represents a bytes type.
	BytesKind = decls.BytesKind

	// DoubleKind represents a double type.
	DoubleKind = decls.DoubleKind

	// DurationKind represents a CEL duration type.
	DurationKind = decls.DurationKind

	// IntKind represents an integer type.
	IntKind = decls.IntKind

	// ListKind represents a list type.
	ListKind = decls.ListKind

	// MapKind represents a map type.
	MapKind = decls.MapKind

	// NullTypeKind represents a null type.
	NullTypeKind = decls.NullTypeKind

	// OpaqueKind represents an abstract type which has no accessible fields.
	OpaqueKind = decls.OpaqueKind

	// StringKind represents a string type.
	StringKind = decls.StringKind

	// StructKind represents a structured object with typed fields.
	StructKind = decls.StructKind

	// TimestampKind represents a a CEL time type.
	TimestampKind = decls.TimestampKind

	// TypeKind represents the CEL type.
	TypeKind = decls.TypeKind

	// TypeParamKind represents a parameterized type whose type name will be resolved at type-check time, if possible.
	TypeParamKind = decls.TypeParamKind

	// UintKind represents a uint type.
	UintKind = decls.UintKind
)

var (
	// AnyType represents the google.protobuf.Any type.
	AnyType = decls.AnyType
	// BoolType represents the bool type.
	BoolType = decls.BoolType
	// BytesType represents the bytes type.
	BytesType = decls.BytesType
	// DoubleType represents the double type.
	DoubleType = decls.DoubleType
	// DurationType represents the CEL duration type.
	DurationType = decls.DurationType
	// DynType represents a dynamic CEL type whose type will be determined at runtime from context.
	DynType = decls.DynType
	// IntType represents the int type.
	IntType = decls.IntType
	// NullType represents the type of a null value.
	NullType = decls.NullType
	// StringType represents the string type.
	StringType = decls.StringType
	// TimestampType represents the time type.
	TimestampType = decls.TimestampType
	// TypeType represents a CEL type
	TypeType = decls.TypeType
	// UintType represents a uint type.
	UintType = decls.UintType

	// function references for instantiating new types.

	// ListType creates an instances of a list type value with the provided element type.
	ListType = decls.ListType
	// MapType creates an instance of a map type value with the provided key and value types.
	MapType = decls.MapType
	// NullableType creates an instance of a nullable type with the provided wrapped type.
	//
	// Note: only primitive types are supported as wrapped types.
	NullableType = decls.NullableType
	// OptionalType creates an abstract parameterized type instance corresponding to CEL's notion of optional.
	OptionalType = decls.OptionalType
	// OpaqueType creates an abstract parameterized type with a given name.
	OpaqueType = decls.OpaqueType
	// ObjectType creates a type references to an externally defined type, e.g. a protobuf message type.
	ObjectType = decls.ObjectType
	// TypeParamType creates a parameterized type instance.
	TypeParamType = decls.TypeParamType
)

// Type holds a reference to a runtime type with an optional type-checked set of type parameters.
type Type = decls.Type

// Variable creates an instance of a variable declaration with a variable name and type.
func Variable(name string, t *Type) EnvOption {
	return func(e *Env) (*Env, error) {
		et, err := TypeToExprType(t)
		if err != nil {
			return nil, err
		}
		e.declarations = append(e.declarations, chkdecls.NewVar(name, et))
		return e, nil
	}
}

// Function defines a function and overloads with optional singleton or per-overload bindings.
//
// Using Function is roughly equivalent to calling Declarations() to declare the function signatures
// and Functions() to define the function bindings, if they have been defined. Specifying the
// same function name more than once will result in the aggregation of the function overloads. If any
// signatures conflict between the existing and new function definition an error will be raised.
// However, if the signatures are identical and the overload ids are the same, the redefinition will
// be considered a no-op.
//
// One key difference with using Function() is that each FunctionDecl provided will handle dynamic
// dispatch based on the type-signatures of the overloads provided which means overload resolution at
// runtime is handled out of the box rather than via a custom binding for overload resolution via
// Functions():
//
// - Overloads are searched in the order they are declared
// - Dynamic dispatch for lists and maps is limited by inspection of the list and map contents
//
//	at runtime. Empty lists and maps will result in a 'default dispatch'
//
// - In the event that a default dispatch occurs, the first overload provided is the one invoked
//
// If you intend to use overloads which differentiate based on the key or element type of a list or
// map, consider using a generic function instead: e.g. func(list(T)) or func(map(K, V)) as this
// will allow your implementation to determine how best to handle dispatch and the default behavior
// for empty lists and maps whose contents cannot be inspected.
//
// For functions which use parameterized opaque types (abstract types), consider using a singleton
// function which is capable of inspecting the contents of the type and resolving the appropriate
// overload as CEL can only make inferences by type-name regarding such types.
func Function(name string, opts ...FunctionOpt) EnvOption {
	return func(e *Env) (*Env, error) {
		fn, err := decls.NewFunction(name, opts...)
		if err != nil {
			return nil, err
		}
		_, err = decls.FunctionDeclToExprDecl(fn)
		if err != nil {
			return nil, err
		}
		if existing, found := e.functions[fn.Name]; found {
			fn, err = existing.Merge(fn)
			if err != nil {
				return nil, err
			}
		}
		e.functions[fn.Name] = fn
		return e, nil
	}
}

// FunctionOpt defines a functional  option for configuring a function declaration.
type FunctionOpt = decls.FunctionOpt

// SingletonUnaryBinding creates a singleton function definition to be used for all function overloads.
//
// Note, this approach works well if operand is expected to have a specific trait which it implements,
// e.g. traits.ContainerType. Otherwise, prefer per-overload function bindings.
func SingletonUnaryBinding(fn functions.UnaryOp, traits ...int) FunctionOpt {
	return decls.SingletonUnaryBinding(fn, traits...)
}

// SingletonBinaryImpl creates a singleton function definition to be used with all function overloads.
//
// Note, this approach works well if operand is expected to have a specific trait which it implements,
// e.g. traits.ContainerType. Otherwise, prefer per-overload function bindings.
//
// Deprecated: use SingletonBinaryBinding
func SingletonBinaryImpl(fn functions.BinaryOp, traits ...int) FunctionOpt {
	return decls.SingletonBinaryBinding(fn, traits...)
}

// SingletonBinaryBinding creates a singleton function definition to be used with all function overloads.
//
// Note, this approach works well if operand is expected to have a specific trait which it implements,
// e.g. traits.ContainerType. Otherwise, prefer per-overload function bindings.
func SingletonBinaryBinding(fn functions.BinaryOp, traits ...int) FunctionOpt {
	return decls.SingletonBinaryBinding(fn, traits...)
}

// SingletonFunctionImpl creates a singleton function definition to be used with all function overloads.
//
// Note, this approach works well if operand is expected to have a specific trait which it implements,
// e.g. traits.ContainerType. Otherwise, prefer per-overload function bindings.
//
// Deprecated: use SingletonFunctionBinding
func SingletonFunctionImpl(fn functions.FunctionOp, traits ...int) FunctionOpt {
	return decls.SingletonFunctionBinding(fn, traits...)
}

// SingletonFunctionBinding creates a singleton function definition to be used with all function overloads.
//
// Note, this approach works well if operand is expected to have a specific trait which it implements,
// e.g. traits.ContainerType. Otherwise, prefer per-overload function bindings.
func SingletonFunctionBinding(fn functions.FunctionOp, traits ...int) FunctionOpt {
	return decls.SingletonFunctionBinding(fn, traits...)
}

// DisableDeclaration disables the function signatures, effectively removing them from the type-check
// environment while preserving the runtime bindings.
func DisableDeclaration(value bool) FunctionOpt {
	return decls.DisableDeclaration(value)
}

// Overload defines a new global overload with an overload id, argument types, and result type. Through the
// use of OverloadOpt options, the overload may also be configured with a binding, an operand trait, and to
// be non-strict.
//
// Note: function bindings should be commonly configured with Overload instances whereas operand traits and
// strict-ness should be rare occurrences.
func Overload(overloadID string, args []*Type, resultType *Type, opts ...OverloadOpt) FunctionOpt {
	return decls.Overload(overloadID, args, resultType, opts...)
}

// MemberOverload defines a new receiver-style overload (or member function) with an overload id, argument types,
// and result type. Through the use of OverloadOpt options, the overload may also be configured with a binding,
// an operand trait, and to be non-strict.
//
// Note: function bindings should be commonly configured with Overload instances whereas operand traits and
// strict-ness should be rare occurrences.
func MemberOverload(overloadID string, args []*Type, resultType *Type, opts ...OverloadOpt) FunctionOpt {
	return decls.MemberOverload(overloadID, args, resultType, opts...)
}

// OverloadOpt is a functional option for configuring a function overload.
type OverloadOpt = decls.OverloadOpt

// UnaryBinding provides the implementation of a unary overload. The provided function is protected by a runtime
// type-guard which ensures runtime type agreement between the overload signature and runtime argument types.
func UnaryBinding(binding functions.UnaryOp) OverloadOpt {
	return decls.UnaryBinding(binding)
}

// BinaryBinding provides the implementation of a binary overload. The provided function is protected by a runtime
// type-guard which ensures runtime type agreement between the overload signature and runtime argument types.
func BinaryBinding(binding functions.BinaryOp) OverloadOpt {
	return decls.BinaryBinding(binding)
}

// FunctionBinding provides the implementation of a variadic overload. The provided function is protected by a runtime
// type-guard which ensures runtime type agreement between the overload signature and runtime argument types.
func FunctionBinding(binding functions.FunctionOp) OverloadOpt {
	return decls.FunctionBinding(binding)
}

// OverloadIsNonStrict enables the function to be called with error and unknown argument values.
//
// Note: do not use this option unless absoluately necessary as it should be an uncommon feature.
func OverloadIsNonStrict() OverloadOpt {
	return decls.OverloadIsNonStrict()
}

// OverloadOperandTrait configures a set of traits which the first argument to the overload must implement in order to be
// successfully invoked.
func OverloadOperandTrait(trait int) OverloadOpt {
	return decls.OverloadOperandTrait(trait)
}

// TypeToExprType converts a CEL-native type representation to a protobuf CEL Type representation.
func TypeToExprType(t *Type) (*exprpb.Type, error) {
	return decls.TypeToExprType(t)
}

// ExprTypeToType converts a protobuf CEL type representation to a CEL-native type representation.
func ExprTypeToType(t *exprpb.Type) (*Type, error) {
	return decls.ExprTypeToType(t)
}

// ExprDeclToDeclaration converts a protobuf CEL declaration to a CEL-native declaration, either a Variable or Function.
func ExprDeclToDeclaration(d *exprpb.Decl) (EnvOption, error) {
	switch d.GetDeclKind().(type) {
	case *exprpb.Decl_Function:
		overloads := d.GetFunction().GetOverloads()
		opts := make([]FunctionOpt, len(overloads))
		for i, o := range overloads {
			args := make([]*Type, len(o.GetParams()))
			for j, p := range o.GetParams() {
				a, err := decls.ExprTypeToType(p)
				if err != nil {
					return nil, err
				}
				args[j] = a
			}
			res, err := decls.ExprTypeToType(o.GetResultType())
			if err != nil {
				return nil, err
			}
			opts[i] = decls.Overload(o.GetOverloadId(), args, res)
		}
		return Function(d.GetName(), opts...), nil
	case *exprpb.Decl_Ident:
		t, err := decls.ExprTypeToType(d.GetIdent().GetType())
		if err != nil {
			return nil, err
		}
		return Variable(d.GetName(), t), nil
	default:
		return nil, fmt.Errorf("unsupported decl: %v", d)
	}
}

func typeValueToKind(tv *types.TypeValue) (Kind, error) {
	switch tv {
	case types.BoolType:
		return BoolKind, nil
	case types.DoubleType:
		return DoubleKind, nil
	case types.IntType:
		return IntKind, nil
	case types.UintType:
		return UintKind, nil
	case types.ListType:
		return ListKind, nil
	case types.MapType:
		return MapKind, nil
	case types.StringType:
		return StringKind, nil
	case types.BytesType:
		return BytesKind, nil
	case types.DurationType:
		return DurationKind, nil
	case types.TimestampType:
		return TimestampKind, nil
	case types.NullType:
		return NullTypeKind, nil
	case types.TypeType:
		return TypeKind, nil
	default:
		switch tv.TypeName() {
		case "dyn":
			return DynKind, nil
		case "google.protobuf.Any":
			return AnyKind, nil
		case "optional":
			return OpaqueKind, nil
		default:
			return 0, fmt.Errorf("no known conversion for type of %s", tv.TypeName())
		}
	}
}
