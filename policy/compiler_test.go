// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policy

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"github.com/google/cel-go/interpreter"

	"github.com/open-policy-agent/opa/rego"
)

func TestCompile(t *testing.T) {
	for _, tst := range policyTests {
		r := newRunner(t, tst.name, tst.expr, tst.parseOpts, tst.envOpts...)
		r.run(t)
	}
}

func TestCompileError(t *testing.T) {
	for _, tst := range policyErrorTests {
		config := readPolicyConfig(t, fmt.Sprintf("testdata/%s/config.yaml", tst.name))
		env := createEnv(t, config, []cel.EnvOption{})
		policy := parsePolicy(t, tst.name, []ParserOption{})
		_, iss := compilePolicy(t, env, policy, tst.compilerOpts)
		if iss.Err() == nil {
			t.Fatalf("compile(%s) did not error, wanted %s", tst.name, tst.err)
		}
		if iss.Err().Error() != tst.err {
			t.Errorf("compile(%s) got error %s, wanted %s", tst.name, iss.Err().Error(), tst.err)
		}
	}
}

func TestCompiledRuleHasOptionalOutput(t *testing.T) {
	env, err := cel.NewEnv()
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	tests := []struct {
		rule     *CompiledRule
		optional bool
	}{
		{rule: &CompiledRule{}, optional: false},
		{
			rule: &CompiledRule{
				matches: []*CompiledMatch{{}},
			},
			optional: true,
		},
		{
			rule: &CompiledRule{
				matches: []*CompiledMatch{{}},
			},
			optional: true,
		},
		{
			rule: &CompiledRule{
				matches: []*CompiledMatch{{cond: mustCompileExpr(t, env, "true")}},
			},
			optional: false,
		},
		{
			rule: &CompiledRule{
				matches: []*CompiledMatch{{cond: mustCompileExpr(t, env, "1 < 0")}},
			},
			optional: true,
		},
	}
	for _, tst := range tests {
		got := tst.rule.HasOptionalOutput()
		if got != tst.optional {
			t.Errorf("rule.HasOptionalOutput() got %v, wanted, %v", got, tst.optional)
		}
	}
}

func TestMaxNestedExpressions_Error(t *testing.T) {
	policyName := "required_labels"
	wantError := `ERROR: testdata/required_labels/policy.yaml:15:8: error configuring compiler option: nested expression limit must be non-negative, non-zero value: -1
 | name: "required_labels"
 | .......^`
	config := readPolicyConfig(t, fmt.Sprintf("testdata/%s/config.yaml", policyName))
	env := createEnv(t, config, []cel.EnvOption{})
	policy := parsePolicy(t, policyName, []ParserOption{})
	_, iss := compilePolicy(t, env, policy, []CompilerOption{MaxNestedExpressions(-1)})
	if iss.Err() == nil {
		t.Fatalf("compile(%s) did not error, wanted %s", policyName, wantError)
	}
	if iss.Err().Error() != wantError {
		t.Errorf("compile(%s) got error %s, wanted %s", policyName, iss.Err().Error(), wantError)
	}
}

func BenchmarkCompile(b *testing.B) {
	for _, tst := range policyTests {
		r := newRunner(b, tst.name, tst.expr, tst.parseOpts, tst.envOpts...)
		r.bench(b)
	}
}

func BenchmarkCompileSetup(b *testing.B) {
	for _, tst := range policyTests {
		tc := tst
		config := readPolicyConfig(b, fmt.Sprintf("testdata/%s/config.yaml", tc.name))
		env := createEnv(b, config, tc.envOpts)
		policy := parsePolicy(b, tc.name, tc.parseOpts)
		ast, iss := compilePolicy(b, env, policy, []CompilerOption{})
		if iss.Err() != nil {
			b.Fatalf("compilePolicy() failed: %v", iss.Err())
		}
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				env.Program(ast, cel.EvalOptions(cel.OptOptimize))
			}
		})
	}
}

func BenchmarkRegoSetup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		r := rego.New(
			rego.Query("data.authz.required_labels"),
			rego.Module("example.rego",
				`package authz

			import rego.v1

			# This definition checks if the costcenter label is not provided. Each rule definition
			# contributes to the set of error messages.
			required_labels contains output if {
				some i, _ in input.spec.labels
				not input.resource.labels[i]
				output := sprintf("missing one or more required labels: %v", [i])
			}
			
			required_labels contains output if {
				some i, v in input.spec.labels
				input.resource.labels[i] != v
				output := sprintf("invalid values provided on one or more labels: %v", [i])
			}
	`,
			),
			rego.Input(map[string]any{
				"spec": map[string]any{
					"labels": map[string]string{
						"env":        "prod",
						"experiment": "group b",
					},
				},
				"resource": map[string]any{
					"labels": map[string]string{
						"env":        "prod",
						"experiment": "group b",
						"release":    "v0.1.0",
					},
				},
			},
			),
		)
		_, err := r.Eval(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRego(b *testing.B) {
	ctx := context.Background()
	r := rego.New(
		rego.Query("data.authz.required_labels"),
		rego.Module("example.rego",
			`package authz

			import rego.v1

			# This definition checks if the costcenter label is not provided. Each rule definition
			# contributes to the set of error messages.
			required_labels contains output if {
				some i, _ in input.spec.labels
				not input.resource.labels[i]
				output := sprintf("missing one or more required labels: %v", [i])
			}
			
			required_labels contains output if {
				some i, v in input.spec.labels
				input.resource.labels[i] != v
				output := sprintf("invalid values provided on one or more labels: %v", [i])
			}
	`,
		),
	)

	prg, err := r.PrepareForEval(ctx)
	if err != nil {
		b.Fatal(err)
	}

	// Run evaluation.
	for i := 0; i < b.N; i++ {
		_, err = prg.Eval(ctx, rego.EvalInput(map[string]any{
			"spec": map[string]any{
				"labels": map[string]any{
					"env":        "prod",
					"experiment": "group b",
				},
			},
			"resource": map[string]any{
				"labels": map[string]string{
					"env":        "prod",
					"experiment": "group b",
					"release":    "v0.1.0",
				},
			},
		},
		))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func newRunner(t testing.TB, name, expr string, parseOpts []ParserOption, opts ...cel.EnvOption) *runner {
	r := &runner{
		name:      name,
		envOpts:   opts,
		parseOpts: parseOpts,
		expr:      expr}
	r.setup(t)
	return r
}

type runner struct {
	name         string
	envOpts      []cel.EnvOption
	parseOpts    []ParserOption
	compilerOpts []CompilerOption
	env          *cel.Env
	expr         string
	prg          cel.Program
}

func mustCompileExpr(t testing.TB, env *cel.Env, expr string) *cel.Ast {
	t.Helper()
	out, iss := env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("env.Compile(%s) failed: %v", expr, iss.Err())
	}
	return out
}

func createEnv(t testing.TB, config *Config, envOpts []cel.EnvOption) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		cel.DefaultUTCTimeZone(true),
		cel.OptionalTypes(),
		cel.EnableMacroCallTracking(),
		cel.ExtendedValidations(),
		ext.Bindings(),
		ext.TwoVarComprehensions())
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	// Configure any custom environment options.
	env, err = env.Extend(envOpts...)
	if err != nil {
		t.Fatalf("env.Extend() with env options %v, failed: %v", config, err)
	}
	// Configure declarations
	configOpts, err := config.AsEnvOptions(env)
	if err != nil {
		t.Fatalf("config.AsEnvOptions() failed: %v", err)
	}
	env, err = env.Extend(configOpts...)
	if err != nil {
		t.Fatalf("env.Extend() with config options %v, failed: %v", config, err)
	}
	return env
}

func parsePolicy(t testing.TB, name string, parseOpts []ParserOption) *Policy {
	policySrc := readPolicy(t, fmt.Sprintf("testdata/%s/policy.yaml", name))
	parser, err := NewParser(parseOpts...)
	if err != nil {
		t.Fatalf("NewParser() failed: %v", err)
	}
	policy, iss := parser.Parse(policySrc)
	if iss.Err() != nil {
		t.Fatalf("Parse() failed: %v", iss.Err())
	}
	if policy.name.Value != name {
		t.Errorf("policy name is %v, wanted %q", policy.name, name)
	}
	return policy
}

func compilePolicy(t testing.TB, env *cel.Env, policy *Policy, compilerOpts []CompilerOption) (*cel.Ast, *cel.Issues) {
	t.Helper()
	return Compile(env, policy, compilerOpts...)
}

func (r *runner) setup(t testing.TB) {
	t.Helper()
	config := readPolicyConfig(t, fmt.Sprintf("testdata/%s/config.yaml", r.name))
	env := createEnv(t, config, r.envOpts)
	policy := parsePolicy(t, r.name, r.parseOpts)
	ast, iss := compilePolicy(t, env, policy, r.compilerOpts)
	if iss.Err() != nil {
		t.Fatalf("Compile() failed: %v", iss.Err())
	}
	// pExpr, err := cel.AstToString(ast)
	// if err != nil {
	// 	t.Fatalf("cel.AstToString() failed: %v", err)
	// }
	// if r.expr != "" && normalize(pExpr) != normalize(r.expr) {
	// 	t.Errorf("cel.AstToString() got %s, wanted %s", pExpr, r.expr)
	// }
	prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}
	r.env = env
	r.prg = prg
}

func (r *runner) run(t *testing.T) {
	tests := readTestSuite(t, fmt.Sprintf("testdata/%s/tests.yaml", r.name))
	for _, s := range tests.Sections {
		section := s.Name
		for _, tst := range s.Tests {
			tc := tst
			t.Run(fmt.Sprintf("%s/%s/%s", r.name, section, tc.Name), func(t *testing.T) {
				input := map[string]any{}
				var err error
				var activation interpreter.Activation
				for k, v := range tc.Input {
					if v.Expr != "" {
						input[k] = r.eval(t, v.Expr)
						continue
					}
					if v.ContextExpr != "" {
						ctx, err := r.eval(t, v.ContextExpr).ConvertToNative(
							reflect.TypeOf(((*proto.Message)(nil))).Elem())
						if err != nil {
							t.Fatalf("context variable is not a valid proto: %v", err)
						}
						activation, err = cel.ContextProtoVars(ctx.(proto.Message))
						if err != nil {
							t.Fatalf("cel.ContextProtoVars() failed: %v", err)
						}
						break
					}
					input[k] = v.Value
				}
				if activation == nil {
					activation, err = interpreter.NewActivation(input)
					if err != nil {
						t.Fatalf("interpreter.NewActivation(input) failed: %v", err)
					}
				}
				out, _, err := r.prg.Eval(activation)
				if err != nil {
					t.Fatalf("prg.Eval(input) failed: %v", err)
				}
				testOut := r.eval(t, tc.Output)
				if optOut, ok := out.(*types.Optional); ok {
					if optOut.Equal(types.OptionalNone) == types.True {
						if testOut.Equal(types.OptionalNone) != types.True {
							t.Errorf("policy eval got %v, wanted %v", out, testOut)
						}
					} else if testOut.Equal(optOut.GetValue()) != types.True {
						t.Errorf("policy eval got %v, wanted %v", out, testOut)
					}
				} else if testOut.Equal(out) != types.True {
					t.Errorf("policy eval got %v, wanted %v", out, testOut)
				}
			})
		}
	}
}

func (r *runner) bench(b *testing.B) {
	tests := readTestSuite(b, fmt.Sprintf("testdata/%s/tests.yaml", r.name))
	for _, s := range tests.Sections {
		section := s.Name
		for _, tst := range s.Tests {
			tc := tst
			b.Run(fmt.Sprintf("%s/%s/%s", r.name, section, tc.Name), func(b *testing.B) {
				input := map[string]any{}
				var err error
				var activation interpreter.Activation
				for k, v := range tc.Input {
					if v.Expr != "" {
						input[k] = r.eval(b, v.Expr)
						continue
					}
					if v.ContextExpr != "" {
						ctx, err := r.eval(b, v.ContextExpr).ConvertToNative(
							reflect.TypeOf(((*proto.Message)(nil))).Elem())
						if err != nil {
							b.Fatalf("context variable is not a valid proto: %v", err)
						}
						activation, err = cel.ContextProtoVars(ctx.(proto.Message))
						if err != nil {
							b.Fatalf("cel.ContextProtoVars() failed: %v", err)
						}
						break
					}
					input[k] = v.Value
				}
				if activation == nil {
					activation, err = interpreter.NewActivation(input)
					if err != nil {
						b.Fatalf("interpreter.NewActivation(input) failed: %v", err)
					}
				}
				for i := 0; i < b.N; i++ {
					_, _, err := r.prg.Eval(activation)
					if err != nil {
						b.Fatalf("policy eval failed: %v", err)
					}
				}
			})
		}
	}
}

func (r *runner) eval(t testing.TB, expr string) ref.Val {
	wantExpr, iss := r.env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("env.Compile(%q) failed :%v", expr, iss.Err())
	}
	prg, err := r.env.Program(wantExpr)
	if err != nil {
		t.Fatalf("env.Program(wantExpr) failed: %v", err)
	}
	out, _, err := prg.Eval(cel.NoVars())
	if err != nil {
		t.Fatalf("prg.Eval() failed: %v", err)
	}
	return out
}

func normalize(s string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(s, " ", ""), "\n", ""),
		"\t", "")
}
