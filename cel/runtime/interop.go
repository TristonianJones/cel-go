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
	"fmt"

	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type interopCELTypeProvider struct {
	ref.TypeProvider
}

func (p *interopCELTypeProvider) FindStructType(typeName string) (*types.Type, bool) {
	t, found := p.TypeProvider.FindType(typeName)
	if !found {
		return nil, false
	}
	ct, err := types.ExprTypeToType(t)
	if err != nil {
		return nil, false
	}
	return ct, true
}

func (p *interopCELTypeProvider) FindStructFieldNames(typeName string) ([]string, bool) {
	return nil, false
}

func (p *interopCELTypeProvider) FindStructFieldType(structType, fieldName string) (*types.FieldType, bool) {
	ft, found := p.TypeProvider.FindFieldType(structType, fieldName)
	if !found {
		return nil, false
	}
	ct, err := types.ExprTypeToType(ft.Type)
	if err != nil {
		return nil, false
	}
	return &types.FieldType{
		Type:    ct,
		IsSet:   ft.IsSet,
		GetFrom: ft.GetFrom,
	}, true
}

type interopLegacyTypeProvider struct {
	types.Provider
}

func (p *interopLegacyTypeProvider) FindType(typeName string) (*exprpb.Type, bool) {
	st, found := p.Provider.FindStructType(typeName)
	if !found {
		return nil, false
	}
	et, err := types.TypeToExprType(st)
	if err != nil {
		return nil, false
	}
	return et, true
}

func (p *interopLegacyTypeProvider) FindFieldType(structType, fieldName string) (*ref.FieldType, bool) {
	ft, found := p.Provider.FindStructFieldType(structType, fieldName)
	if !found {
		return nil, false
	}
	et, err := types.TypeToExprType(ft.Type)
	if err != nil {
		return nil, false
	}
	return &ref.FieldType{
		Type:    et,
		IsSet:   ft.IsSet,
		GetFrom: ft.GetFrom,
	}, true
}

func maybeInteropProvider(provider any) (types.Provider, error) {
	switch p := provider.(type) {
	case types.Provider:
		return p, nil
	case ref.TypeProvider:
		return &interopCELTypeProvider{TypeProvider: p}, nil
	default:
		return nil, fmt.Errorf("unsupported type provider: %T", provider)
	}
}
