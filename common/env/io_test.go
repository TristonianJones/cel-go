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

package env

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseTypeDesc(t *testing.T) {
	tcs := []struct {
		text string
		want *TypeDesc
	}{
		{
			"int",
			NewTypeDesc("int"),
		},
		{
			"foo",
			NewTypeDesc("foo"),
		},
		{
			".com.example.Message",
			NewTypeDesc(".com.example.Message"),
		},
		{
			"list<int>",
			NewTypeDesc("list", NewTypeDesc("int")),
		},
		{
			" list < int > ",
			NewTypeDesc("list", NewTypeDesc("int")),
		},
		{
			"map<int, list<string>>",
			NewTypeDesc("map", NewTypeDesc("int"), NewTypeDesc("list", NewTypeDesc("string"))),
		},
		{
			"list<\n\tint\n>",
			NewTypeDesc("list", NewTypeDesc("int")),
		},
		{
			"map<\r\n  string,\r\n  list<string>\r\n>",
			NewTypeDesc("map", NewTypeDesc("string"), NewTypeDesc("list", NewTypeDesc("string"))),
		},
		{
			"map<string,\tint>",
			NewTypeDesc("map", NewTypeDesc("string"), NewTypeDesc("int")),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseTypeDesc(tc.text)
			if err != nil {
				t.Fatalf("ParseTypeDesc(%q) = <err> %v, want nil", tc.text, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseTypeDesc(%q) = %s diff (-want +got):\n%s", tc.text, got, diff)
			}
		})
	}
}

func TestParseTypeDescErrors(t *testing.T) {
	tcs := []struct {
		text    string
		wantErr string
	}{
		{
			"",
			"missing identifier at position 0",
		},
		{
			"int int",
			"nexpected character 'i'",
		},
		{
			"int>",
			"unexpected character '>'",
		},
		{
			".foo.",
			"unexpected end of input",
		},
		{
			"..foo",
			"identifier is expected, but '.' was found at position 1",
		},
		{
			"~",
			"unexpected end of input",
		},
		{
			"~1",
			"invalid type parameter identifier '1' at position 1",
		},
		{
			"~elem",
			"invalid type param, must have a single alphabetic character at position 2",
		},
		{
			"list<",
			"missing identifier at position 5",
		},
		{
			"list<int",
			"expected ',' or '>' at position 8",
		},
		{
			"list<~>",
			"invalid type parameter identifier '>'",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.text, func(t *testing.T) {
			_, err := ParseTypeDesc(tc.text)
			if err == nil {
				t.Fatalf("ParseTypeDesc(%q) = nil, wanted err %v", tc.text, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ParseTypeDesc(%q) = %v, wanted err %v", tc.text, err, tc.wantErr)
			}
		})
	}
}

func TestConfigToYAML(t *testing.T) {
	// Parsing will accept shorthand for type specifiers, but we always output the
	// structured map nodes so not fully reversible.
	//
	tcs := []struct {
		name    string
		confIn  *Config
		yamlOut string
	}{
		{
			name: "string variable type",
			confIn: NewConfig("foo").AddVariables(
				NewVariable("foo", NewTypeDesc("int")),
			),
			yamlOut: `name: foo
variables:
    - name: foo
      type_name: int
`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ConfigToYAML(tc.confIn)
			if err != nil {
				t.Fatalf("ConfigToYAML() = (err) %v", err)
			}
			if diff := cmp.Diff(tc.yamlOut, string(b)); diff != "" {
				t.Errorf("ConfigToYAML() has diff (+got -want):\n%s", diff)
			}
		})
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	// Parsing will accept shorthand for type specifiers, but we always output the
	// structured map nodes so not fully reversible.
	//
	tcs := []struct {
		name    string
		yamlIn  string
		yamlOut string
	}{
		{
			name: "string variable type",
			yamlIn: `name: foo
variables:
    - name: foo
      type: int
`,
			yamlOut: `name: foo
variables:
    - name: foo
      type_name: int
`,
		},
		{
			name: "structured variable type",
			yamlIn: `name: foo
variables:
    - name: foo
      type_name: int
`,
			yamlOut: `name: foo
variables:
    - name: foo
      type_name: int
`,
		},
		{
			name: "map specifier type",
			yamlIn: `name: foo
variables:
    - name: foo
      type: map<int, string>
`,
			yamlOut: `name: foo
variables:
    - name: foo
      type_name: map
      params:
        - type_name: int
        - type_name: string
`,
		},
		{
			name: "function",
			yamlIn: `name: foo
functions:
    - name: getOrDefault
      overloads:
          - id: getOrDefault
            target: map<string, ~V>
            params:
                - string
                - ~V
            return: ~V
`,
			yamlOut: `name: foo
functions:
    - name: getOrDefault
      overloads:
        - id: getOrDefault
          target:
            type_name: map
            params:
                - type_name: string
                - type_name: V
                  is_type_param: true
          return:
            type_name: V
            is_type_param: true
`,
		},
		{
			name: "multiline and tab whitespace in types",
			yamlIn: `name: user_env
variables:
  - name: user_lookup_table
    type: >-
      map<
        string,
        list<string>
      >
  - name: permissions
    type: "map<string,\tint>"
`,
			yamlOut: `name: user_env
variables:
    - name: user_lookup_table
      type_name: map
      params:
        - type_name: string
        - type_name: list
          params:
            - type_name: string
    - name: permissions
      type_name: map
      params:
        - type_name: string
        - type_name: int
`,
		},
		{
			name: "context variable shorthand and overload shorthand",
			yamlIn: `name: subset_env
context_variable:
  type: google.expr.proto3.test.TestAllTypes
stdlib:
  exclude_functions:
    - name: "_+_"
      overloads:
        - add_bytes
`,
			yamlOut: `name: subset_env
stdlib:
    exclude_functions:
        - name: _+_
          overloads:
            - id: add_bytes
context_variable:
    type_name: google.expr.proto3.test.TestAllTypes
`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ConfigFromYAML([]byte(tc.yamlIn))
			if err != nil {
				t.Fatalf("ConfigFromYAML() = (err) %v, want nil", err)
			}
			b, err := ConfigToYAML(c)
			if err != nil {
				t.Fatalf("ConfigToYAML() = (err) %v, wanted nil", err)
			}
			if diff := cmp.Diff(tc.yamlOut, string(b)); diff != "" {
				t.Errorf("ConfigToYAML() has diff (+got -want):\n%s", diff)
			}
		})
	}
}

func TestConfigToJSON(t *testing.T) {
	tcs := []struct {
		name    string
		confIn  *Config
		jsonOut string
	}{
		{
			name: "string variable type",
			confIn: NewConfig("foo").AddVariables(
				NewVariable("foo", NewTypeDesc("int")),
			),
			jsonOut: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "int"
    }
  ]
}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ConfigToJSON(tc.confIn)
			if err != nil {
				t.Fatalf("ConfigToJSON() = (err) %v", err)
			}
			if diff := cmp.Diff(tc.jsonOut, string(b)); diff != "" {
				t.Errorf("ConfigToJSON() has diff (-want +got):\n%s", diff)
			}
		})
	}
	var nilTD *TypeDesc
	if b, err := nilTD.MarshalJSON(); err != nil || string(b) != "null" {
		t.Errorf("nil TypeDesc.MarshalJSON() = %q, %v, want \"null\", nil", string(b), err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	// Parsing will accept both shorthand and structured type specifiers, and
	// outputs shorthand specifier strings in JSON.
	tcs := []struct {
		name    string
		jsonIn  string
		jsonOut string
	}{
		{
			name: "string variable type",
			jsonIn: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "int"
    }
  ]
}`,
			jsonOut: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "int"
    }
  ]
}`,
		},
		{
			name: "structured variable type",
			jsonIn: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "typeName": "int"
    }
  ]
}`,
			jsonOut: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "int"
    }
  ]
}`,
		},
		{
			name: "nested structured variable type",
			jsonIn: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": {
        "typeName": "list",
        "params": [
          {
            "typeName": "string"
          }
        ]
      }
    }
  ]
}`,
			jsonOut: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "list<string>"
    }
  ]
}`,
		},
		{
			name: "map specifier type",
			jsonIn: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "map<int, string>"
    }
  ]
}`,
			jsonOut: `{
  "name": "foo",
  "variables": [
    {
      "name": "foo",
      "type": "map<int, string>"
    }
  ]
}`,
		},
		{
			name: "function with shorthand and structured types",
			jsonIn: `{
  "name": "foo",
  "functions": [
    {
      "name": "getOrDefault",
      "overloads": [
        {
          "id": "getOrDefault",
          "target": "map<string, ~V>",
          "args": [
            "string",
            {
              "typeName": "V",
              "isTypeParam": true
            }
          ],
          "return": "~V"
        }
      ]
    }
  ]
}`,
			jsonOut: `{
  "name": "foo",
  "functions": [
    {
      "name": "getOrDefault",
      "overloads": [
        {
          "id": "getOrDefault",
          "target": "map<string, ~V>",
          "args": [
            "string",
            "~V"
          ],
          "return": "~V"
        }
      ]
    }
  ]
}`,
		},
		{
			name: "multiline and tab whitespace in types",
			jsonIn: `{
  "name": "user_env",
  "variables": [
    {
      "name": "user_lookup_table",
      "type": "map<\n  string,\n  list<string>\n>"
    },
    {
      "name": "permissions",
      "type": "map<string,\tint>"
    }
  ]
}`,
			jsonOut: `{
  "name": "user_env",
  "variables": [
    {
      "name": "user_lookup_table",
      "type": "map<string, list<string>>"
    },
    {
      "name": "permissions",
      "type": "map<string, int>"
    }
  ]
}`,
		},
		{
			name: "context variable type alias and library subset string overloads",
			jsonIn: `{
  "name": "subset_env",
  "stdlib": {
    "excludeFunctions": [
      {
        "name": "_+_",
        "overloads": [
          "add_bytes",
          {
            "id": "add_list"
          }
        ]
      }
    ]
  },
  "contextVariable": {
    "type": "google.expr.proto3.test.TestAllTypes"
  }
}`,
			jsonOut: `{
  "name": "subset_env",
  "stdlib": {
    "excludeFunctions": [
      {
        "name": "_+_",
        "overloads": [
          {
            "id": "add_bytes"
          },
          {
            "id": "add_list"
          }
        ]
      }
    ]
  },
  "contextVariable": {
    "typeName": "google.expr.proto3.test.TestAllTypes"
  }
}`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ConfigFromJSON([]byte(tc.jsonIn))
			if err != nil {
				t.Fatalf("ConfigFromJSON() = (err) %v, want nil", err)
			}
			b, err := ConfigToJSON(c)
			if err != nil {
				t.Fatalf("ConfigToJSON() = (err) %v, wanted nil", err)
			}
			if diff := cmp.Diff(tc.jsonOut, string(b)); diff != "" {
				t.Errorf("ConfigToJSON() has diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfigFromJSONErrors(t *testing.T) {
	tcs := []struct {
		name    string
		jsonIn  string
		wantErr string
	}{
		{
			name:    "invalid json",
			jsonIn:  `{"name": `,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "invalid variable json",
			jsonIn:  `{"variables": ["not-an-object"]}`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "unsupported json type for TypeDesc",
			jsonIn:  `{"variables": [{"name": "x", "type": 123}]}`,
			wantErr: "unsupported type description encoding",
		},
		{
			name:    "invalid type specifier in TypeDesc",
			jsonIn:  `{"variables": [{"name": "x", "type": "list<"}]}`,
			wantErr: "failed to parse type",
		},
		{
			name:    "invalid structured TypeDesc",
			jsonIn:  `{"variables": [{"name": "x", "type": {"typeName": 123}}]}`,
			wantErr: "cannot unmarshal number",
		},
		{
			name:    "invalid validator json",
			jsonIn:  `{"validators": ["not-an-object"]}`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "invalid context variable json",
			jsonIn:  `{"contextVariable": "not-an-object"}`,
			wantErr: "cannot unmarshal string",
		},
		{
			name:    "unsupported json type for Overload",
			jsonIn:  `{"functions": [{"name": "f", "overloads": [123]}]}`,
			wantErr: "unsupported overload encoding",
		},
		{
			name:    "invalid structured Overload",
			jsonIn:  `{"functions": [{"name": "f", "overloads": [{"id": 123}]}]}`,
			wantErr: "cannot unmarshal number",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromJSON([]byte(tc.jsonIn))
			if err == nil {
				t.Fatalf("ConfigFromJSON(%q) = nil, wanted error containing %q", tc.jsonIn, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ConfigFromJSON(%q) = %v, wanted error containing %q", tc.jsonIn, err, tc.wantErr)
			}
		})
	}
}

func TestStrictDecodingYAML(t *testing.T) {
	validInputs := []struct {
		name   string
		yamlIn string
	}{
		{
			name:   "empty",
			yamlIn: "",
		},
		{
			name: "shorthand and structured fields",
			yamlIn: `name: subset_env
context_variable:
  type: google.expr.proto3.test.TestAllTypes
stdlib:
  exclude_functions:
    - name: "_+_"
      overloads:
        - add_bytes
        - id: add_list
variables:
  - name: x
    type: list<int>
  - name: y
    type:
      type_name: list
      params:
        - type_name: string
validators:
  - name: cel.validator.comprehension_nesting_limit
    config:
      limit: 2
`,
		},
	}

	for _, tc := range validInputs {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if _, err := ConfigFromYAML([]byte(tc.yamlIn), StrictDecoding(true)); err != nil {
				t.Fatalf("ConfigFromYAML(StrictDecoding(true)) = %v, want nil", err)
			}
		})
	}

	unknownFieldInputs := []struct {
		name    string
		yamlIn  string
		wantErr string
	}{
		{
			name:    "unknown config field",
			yamlIn:  "name: foo\nunknown_field: bar\n",
			wantErr: "field unknown_field not found",
		},
		{
			name: "unknown variable field",
			yamlIn: `variables:
  - name: x
    type: int
    unknown_var_field: 1
`,
			wantErr: "field unknown_var_field not found",
		},
		{
			name: "unknown overload field",
			yamlIn: `functions:
  - name: getOrDefault
    overloads:
      - id: getOrDefault
        target: map<string, ~V>
        params:
          - string
          - ~V
        return: ~V
`,
			wantErr: "field params not found",
		},
		{
			name: "unknown type_desc field",
			yamlIn: `variables:
  - name: x
    type:
      type_name: list
      params:
        - type_name: int
          unknown_type_field: true
`,
			wantErr: "field unknown_type_field not found",
		},
		{
			name: "unknown context_variable field",
			yamlIn: `context_variable:
  type_name: foo.Bar
  unknown_ctx_field: true
`,
			wantErr: "field unknown_ctx_field not found",
		},
		{
			name: "unknown validator field",
			yamlIn: `validators:
  - name: cel.validator.matches
    unknown_validator_field: true
`,
			wantErr: "field unknown_validator_field not found",
		},
	}

	for _, tc := range unknownFieldInputs {
		t.Run("unknown/"+tc.name, func(t *testing.T) {
			if _, err := ConfigFromYAML([]byte(tc.yamlIn)); err != nil {
				t.Fatalf("ConfigFromYAML() default = %v, want nil", err)
			}
			if _, err := ConfigFromYAML([]byte(tc.yamlIn), StrictDecoding(false)); err != nil {
				t.Fatalf("ConfigFromYAML(StrictDecoding(false)) = %v, want nil", err)
			}
			_, err := ConfigFromYAML([]byte(tc.yamlIn), StrictDecoding(true))
			if err == nil {
				t.Fatalf("ConfigFromYAML(StrictDecoding(true)) = nil, wanted error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ConfigFromYAML(StrictDecoding(true)) = %v, wanted error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestStrictDecodingJSON(t *testing.T) {
	validJSON := `{
  "name": "subset_env",
  "stdlib": {
    "excludeFunctions": [
      {
        "name": "_+_",
        "overloads": [
          "add_bytes",
          {
            "id": "add_list",
            "args": [
              "list<~T>",
              {
                "typeName": "list",
                "params": [
                  {
                    "typeName": "T",
                    "isTypeParam": true
                  }
                ]
              }
            ],
            "return": "list<~T>"
          }
        ]
      }
    ]
  },
  "contextVariable": {
    "type": "google.expr.proto3.test.TestAllTypes"
  },
  "validators": [
    {
      "name": "cel.validator.comprehension_nesting_limit",
      "config": {
        "limit": 2
      }
    }
  ]
}`
	if _, err := ConfigFromJSON([]byte(validJSON), StrictDecoding(true)); err != nil {
		t.Fatalf("ConfigFromJSON(StrictDecoding(true)) = %v, want nil", err)
	}

	unknownFieldInputs := []struct {
		name    string
		jsonIn  string
		wantErr string
	}{
		{
			name:    "unknown config field",
			jsonIn:  `{"name": "foo", "unknownField": "bar"}`,
			wantErr: `unknown field "unknownField"`,
		},
		{
			name:    "unknown variable field",
			jsonIn:  `{"variables": [{"name": "x", "type": "int", "unknownVarField": 1}]}`,
			wantErr: `unknown field "unknownVarField"`,
		},
		{
			name:    "unknown overload field",
			jsonIn:  `{"functions": [{"name": "f", "overloads": [{"id": "f_int", "params": ["int"], "return": "int"}]}]}`,
			wantErr: `unknown field "params"`,
		},
		{
			name:    "unknown typeDesc field",
			jsonIn:  `{"variables": [{"name": "x", "type": {"typeName": "list", "params": [{"typeName": "int", "unknownTypeField": true}]}}]}`,
			wantErr: `unknown field "unknownTypeField"`,
		},
		{
			name:    "unknown contextVariable field",
			jsonIn:  `{"contextVariable": {"typeName": "foo.Bar", "unknownCtxField": true}}`,
			wantErr: `unknown field "unknownCtxField"`,
		},
		{
			name:    "unknown validator field",
			jsonIn:  `{"validators": [{"name": "cel.validator.matches", "unknownValidatorField": true}]}`,
			wantErr: `unknown field "unknownValidatorField"`,
		},
	}

	for _, tc := range unknownFieldInputs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ConfigFromJSON([]byte(tc.jsonIn)); err != nil {
				t.Fatalf("ConfigFromJSON() default = %v, want nil", err)
			}
			if _, err := ConfigFromJSON([]byte(tc.jsonIn), StrictDecoding(false)); err != nil {
				t.Fatalf("ConfigFromJSON(StrictDecoding(false)) = %v, want nil", err)
			}
			_, err := ConfigFromJSON([]byte(tc.jsonIn), StrictDecoding(true))
			if err == nil {
				t.Fatalf("ConfigFromJSON(StrictDecoding(true)) = nil, wanted error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ConfigFromJSON(StrictDecoding(true)) = %v, wanted error containing %q", err, tc.wantErr)
			}
		})
	}
}

