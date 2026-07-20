// Copyright 2020 Google LLC
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

package examples

import (
	"context"
	"fmt"
	"log"

	"github.com/google/cel-go/cel"
)

func Example_cel_Compile() {
	prg, err := cel.Compile(`"Hello world! I'm " + name + "."`,
		cel.Variable("name", cel.StringType),
	)
	if err != nil {
		log.Fatalln(err)
	}
	out, _, err := prg.Eval(map[string]any{
		"name": "CEL",
	})
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(out)

	// Output:
	// Hello world! I'm CEL.
}

func Example_cel_Compile_options() {
	prg, err := cel.Compile(`x > 0 ? x * y : 0`,
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	)
	if err != nil {
		log.Fatalln(err)
	}
	ctx := context.Background()
	out, _, err := prg.ContextEval(ctx, map[string]any{
		"x": 10,
		"y": 5,
	})
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(out)

	// Output:
	// 50
}
