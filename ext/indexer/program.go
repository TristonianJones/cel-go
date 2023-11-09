// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package indexer

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
)

type IndexedProgram struct {
	idxAST *IndexedAST
}

func (prg *IndexedProgram) Eval(vars any) (ref.Val, *cel.EvalDetails, error) {
	return nil, nil, nil
}

func (prg *IndexedProgram) calculateMask(vars interpreter.Activation) uint8 {
	return 0
}
