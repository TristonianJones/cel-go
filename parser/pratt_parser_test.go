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

package parser

import (
	"fmt"
	"strings"
	"testing"

	"cel.dev/cel-go/common"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/debug"
	"cel.dev/cel-go/test"
)

var prattTestCases = []testInfo{
	// Constants
	{
		I: `42`,
		P: `42^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `0x2A`,
		P: `42^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `42u`,
		P: `42u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `0x2Au`,
		P: `42u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `3.14`,
		P: `3.14^#1:*expr.Constant_DoubleValue#`,
	},
	{
		I: `-42`,
		P: `-42^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `-3.14`,
		P: `-3.14^#1:*expr.Constant_DoubleValue#`,
	},
	{
		I: `"hello world"`,
		P: `"hello world"^#1:*expr.Constant_StringValue#`,
	},
	{
		I: `b"bytes"`,
		P: `b"bytes"^#1:*expr.Constant_BytesValue#`,
	},
	{
		I: `true`,
		P: `true^#1:*expr.Constant_BoolValue#`,
	},
	{
		I: `false`,
		P: `false^#1:*expr.Constant_BoolValue#`,
	},
	{
		I: `null`,
		P: `null^#1:*expr.Constant_NullValue#`,
	},
	{
		I: `9223372036854775807`,
		P: `9223372036854775807^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `-9223372036854775808`,
		P: `-9223372036854775808^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `-0x1A`,
		P: `-26^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `-0X1a`,
		P: `-26^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `-0x8000000000000000`,
		P: `-9223372036854775808^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `0u`,
		P: `0u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `-5.5e-3`,
		P: `-0.0055^#1:*expr.Constant_DoubleValue#`,
	},
	{
		I: "\"\u2764\"",
		P: "\"❤\"^#1:*expr.Constant_StringValue#",
	},
	{
		I: "\"\\a\\b\\f\\n\\r\\t\\v'\\\"\\\\\\? Legal escapes\"",
		P: `"\a\b\f\n\r\t\v'\"\\? Legal escapes"^#1:*expr.Constant_StringValue#`,
	},

	// Identifiers and Parentheses
	{
		I: `a`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},
	{
		I: `(a)`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},
	{
		I: `((a))`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},

	// Unary operators
	{
		I: `!a`,
		P: `!_(
			a^#2:*expr.Expr_IdentExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `!false`,
		P: `!_(
			false^#2:*expr.Constant_BoolValue#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `-x`,
		P: `-_(
			x^#2:*expr.Expr_IdentExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `---a`,
		P: `-_(
			-_(
				-_(
					a^#4:*expr.Expr_IdentExpr#
				)^#3:*expr.Expr_CallExpr#
			)^#2:*expr.Expr_CallExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `- -1`,
		P: `-_(
			-1^#2:*expr.Constant_Int64Value#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `4--4`,
		P: `_-_(
			4^#1:*expr.Constant_Int64Value#,
			-4^#3:*expr.Constant_Int64Value#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `!!a`,
		P: `!_(
			!_(
				a^#3:*expr.Expr_IdentExpr#
			)^#2:*expr.Expr_CallExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `-!true`,
		P: `-_(
			!_(
				true^#3:*expr.Constant_BoolValue#
			)^#2:*expr.Expr_CallExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `-!-x`,
		P: `-_(
			!_(
				-_(
					x^#4:*expr.Expr_IdentExpr#
				)^#3:*expr.Expr_CallExpr#
			)^#2:*expr.Expr_CallExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},

	// Binary & Ternary operators
	{
		I: `1 + 2 * 3 - 4 / 2 % 3`,
		P: `_-_(
			_+_(
				1^#1:*expr.Constant_Int64Value#,
				_*_(
					2^#3:*expr.Constant_Int64Value#,
					3^#5:*expr.Constant_Int64Value#
				)^#4:*expr.Expr_CallExpr#
			)^#2:*expr.Expr_CallExpr#,
			_%_(
				_/_(
					4^#7:*expr.Constant_Int64Value#,
					2^#9:*expr.Constant_Int64Value#
				)^#8:*expr.Expr_CallExpr#,
				3^#11:*expr.Constant_Int64Value#
			)^#10:*expr.Expr_CallExpr#
		)^#6:*expr.Expr_CallExpr#`,
	},
	{
		I: `a < 10 && b <= 20 || c > 30 && d >= 40 || e == 50 && f != 60`,
		P: `_||_(
			_||_(
				_&&_(
					_<_(
						a^#1:*expr.Expr_IdentExpr#,
						10^#3:*expr.Constant_Int64Value#
					)^#2:*expr.Expr_CallExpr#,
					_<=_(
						b^#5:*expr.Expr_IdentExpr#,
						20^#7:*expr.Constant_Int64Value#
					)^#6:*expr.Expr_CallExpr#
				)^#4:*expr.Expr_CallExpr#,
				_&&_(
					_>_(
						c^#9:*expr.Expr_IdentExpr#,
						30^#11:*expr.Constant_Int64Value#
					)^#10:*expr.Expr_CallExpr#,
					_>=_(
						d^#13:*expr.Expr_IdentExpr#,
						40^#15:*expr.Constant_Int64Value#
					)^#14:*expr.Expr_CallExpr#
				)^#12:*expr.Expr_CallExpr#
			)^#8:*expr.Expr_CallExpr#,
			_&&_(
				_==_(
					e^#17:*expr.Expr_IdentExpr#,
					50^#19:*expr.Constant_Int64Value#
				)^#18:*expr.Expr_CallExpr#,
				_!=_(
					f^#21:*expr.Expr_IdentExpr#,
					60^#23:*expr.Constant_Int64Value#
				)^#22:*expr.Expr_CallExpr#
			)^#20:*expr.Expr_CallExpr#
		)^#16:*expr.Expr_CallExpr#`,
	},
	{
		I: `a && b && c && d`,
		P: `_&&_(
			_&&_(
				a^#1:*expr.Expr_IdentExpr#,
				b^#3:*expr.Expr_IdentExpr#
			)^#2:*expr.Expr_CallExpr#,
			_&&_(
				c^#5:*expr.Expr_IdentExpr#,
				d^#7:*expr.Expr_IdentExpr#
			)^#6:*expr.Expr_CallExpr#
		)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `a && b && c && d`,
		P: `_&&_(
			a^#1:*expr.Expr_IdentExpr#,
			b^#3:*expr.Expr_IdentExpr#,
			c^#5:*expr.Expr_IdentExpr#,
			d^#7:*expr.Expr_IdentExpr#
		)^#2:*expr.Expr_CallExpr#`,
		Opts: []Option{EnableVariadicOperatorASTs(true)},
	},
	{
		I: `a || b || c || d`,
		P: `_||_(
			_||_(
				a^#1:*expr.Expr_IdentExpr#,
				b^#3:*expr.Expr_IdentExpr#
			)^#2:*expr.Expr_CallExpr#,
			_||_(
				c^#5:*expr.Expr_IdentExpr#,
				d^#7:*expr.Expr_IdentExpr#
			)^#6:*expr.Expr_CallExpr#
		)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `a || b || c || d`,
		P: `_||_(
			a^#1:*expr.Expr_IdentExpr#,
			b^#3:*expr.Expr_IdentExpr#,
			c^#5:*expr.Expr_IdentExpr#,
			d^#7:*expr.Expr_IdentExpr#
		)^#2:*expr.Expr_CallExpr#`,
		Opts: []Option{EnableVariadicOperatorASTs(true)},
	},
	{
		I: `10 - 3 - 2`,
		P: `_-_(
			_-_(
				10^#1:*expr.Constant_Int64Value#,
				3^#3:*expr.Constant_Int64Value#
			)^#2:*expr.Expr_CallExpr#,
			2^#5:*expr.Constant_Int64Value#
		)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `(((10 - 3) - 2))`,
		P: `_-_(
			_-_(
				10^#1:*expr.Constant_Int64Value#,
				3^#3:*expr.Constant_Int64Value#
			)^#2:*expr.Expr_CallExpr#,
			2^#5:*expr.Constant_Int64Value#
		)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `x in [1, 2, 3]`,
		P: `@in(
			x^#1:*expr.Expr_IdentExpr#,
			[
				1^#4:*expr.Constant_Int64Value#,
				2^#5:*expr.Constant_Int64Value#,
				3^#6:*expr.Constant_Int64Value#
			]^#3:*expr.Expr_ListExpr#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a ? b : c`,
		P: `_?_:_(
			a^#1:*expr.Expr_IdentExpr#,
			b^#3:*expr.Expr_IdentExpr#,
			c^#4:*expr.Expr_IdentExpr#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a ? b : c ? d : e`,
		P: `_?_:_(
			a^#1:*expr.Expr_IdentExpr#,
			b^#3:*expr.Expr_IdentExpr#,
			_?_:_(
				c^#4:*expr.Expr_IdentExpr#,
				d^#6:*expr.Expr_IdentExpr#,
				e^#7:*expr.Expr_IdentExpr#
			)^#5:*expr.Expr_CallExpr#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `(((1 + 2))) * (3 + 4)`,
		P: `_*_(
			_+_(
				1^#1:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#
			)^#2:*expr.Expr_CallExpr#,
			_+_(
				3^#5:*expr.Constant_Int64Value#,
				4^#7:*expr.Constant_Int64Value#
			)^#6:*expr.Expr_CallExpr#
		)^#4:*expr.Expr_CallExpr#`,
	},

	// Members, Selects, Indexing, and Calls
	{
		I: `a.b()`,
		P: `a^#1:*expr.Expr_IdentExpr#.b()^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a.b(1)`,
		P: `a^#1:*expr.Expr_IdentExpr#.b(
			1^#3:*expr.Constant_Int64Value#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a.b(1, 2)`,
		P: `a^#1:*expr.Expr_IdentExpr#.b(
			1^#3:*expr.Constant_Int64Value#,
			2^#4:*expr.Constant_Int64Value#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `"foo".size()`,
		P: `"foo"^#1:*expr.Constant_StringValue#.size()^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a.b.c`,
		P: `a^#1:*expr.Expr_IdentExpr#.b^#2:*expr.Expr_SelectExpr#.c^#3:*expr.Expr_SelectExpr#`,
	},
	{
		I: `a[0]`,
		P: `_[_](
			a^#1:*expr.Expr_IdentExpr#,
			0^#3:*expr.Constant_Int64Value#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `[1, 3, 4][0]`,
		P: `_[_](
			[
				1^#2:*expr.Constant_Int64Value#,
				3^#3:*expr.Constant_Int64Value#,
				4^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			0^#6:*expr.Constant_Int64Value#
		)^#5:*expr.Expr_CallExpr#`,
	},
	{
		I: `a[b[c]]`,
		P: `_[_](
			a^#1:*expr.Expr_IdentExpr#,
			_[_](
				b^#3:*expr.Expr_IdentExpr#,
				c^#5:*expr.Expr_IdentExpr#
			)^#4:*expr.Expr_CallExpr#
		)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `a()`,
		P: `a()^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `a(b)`,
		P: `a(
			b^#2:*expr.Expr_IdentExpr#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `func(1, 2, 3)`,
		P: `func(
			1^#2:*expr.Constant_Int64Value#,
			2^#3:*expr.Constant_Int64Value#,
			3^#4:*expr.Constant_Int64Value#
		)^#1:*expr.Expr_CallExpr#`,
	},
	{
		I: `.func(1, 2)`,
		P: `.func(
			1^#2:*expr.Constant_Int64Value#,
			2^#3:*expr.Constant_Int64Value#
		)^#1:*expr.Expr_CallExpr#`,
	},

	// Collections & Structs
	{
		I: `[1, 2, 3]`,
		P: `[
			1^#2:*expr.Constant_Int64Value#,
			2^#3:*expr.Constant_Int64Value#,
			3^#4:*expr.Constant_Int64Value#
		]^#1:*expr.Expr_ListExpr#`,
	},
	{
		I: `[1, 2, 3,]`,
		P: `[
			1^#2:*expr.Constant_Int64Value#,
			2^#3:*expr.Constant_Int64Value#,
			3^#4:*expr.Constant_Int64Value#
		]^#1:*expr.Expr_ListExpr#`,
	},
	{
		I: `[]`,
		P: `[]^#1:*expr.Expr_ListExpr#`,
	},
	{
		I: `{"a": 1, "b": 2}`,
		P: `{
			"a"^#2:*expr.Constant_StringValue#:1^#4:*expr.Constant_Int64Value#^#3:*expr.Expr_CreateStruct_Entry#,
			"b"^#5:*expr.Constant_StringValue#:2^#7:*expr.Constant_Int64Value#^#6:*expr.Expr_CreateStruct_Entry#
		}^#1:*expr.Expr_StructExpr#`,
	},
	{
		I: `{foo: 5, bar: "xyz"}`,
		P: `{
			foo^#2:*expr.Expr_IdentExpr#:5^#4:*expr.Constant_Int64Value#^#3:*expr.Expr_CreateStruct_Entry#,
			bar^#5:*expr.Expr_IdentExpr#:"xyz"^#7:*expr.Constant_StringValue#^#6:*expr.Expr_CreateStruct_Entry#
		}^#1:*expr.Expr_StructExpr#`,
	},
	{
		I: `{}`,
		P: `{}^#1:*expr.Expr_StructExpr#`,
	},
	{
		I: `google.protobuf.Empty{}`,
		P: `google.protobuf.Empty{}^#3:*expr.Expr_StructExpr#`,
	},
	{
		I: `foo{ a: b }`,
		P: `foo{
			a:b^#2:*expr.Expr_IdentExpr#^#1:*expr.Expr_CreateStruct_Entry#
		}^#1:*expr.Expr_StructExpr#`,
	},
	{
		I: `pkg.Msg{field1: "val", field2: 42}`,
		P: `pkg.Msg{
			field1:"val"^#3:*expr.Constant_StringValue#^#2:*expr.Expr_CreateStruct_Entry#,
			field2:42^#5:*expr.Constant_Int64Value#^#4:*expr.Expr_CreateStruct_Entry#
		}^#2:*expr.Expr_StructExpr#`,
	},
	{
		I: `.pkg.Msg{field1: "val"}`,
		P: `.pkg.Msg{
			field1:"val"^#3:*expr.Constant_StringValue#^#2:*expr.Expr_CreateStruct_Entry#
		}^#2:*expr.Expr_StructExpr#`,
	},

	// Macros
	{
		I: `has(a.b)`,
		P: `a^#2:*expr.Expr_IdentExpr#.b~test-only~^#4:has#`,
	},
	{
		I: `[1, 2, 3].all(x, x > 0)`,
		P: `__comprehension__(
			// Variable
			x,
			// Target
			[
				1^#2:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#,
				3^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			// Accumulator
			@result,
			// Init
			true^#10:*expr.Constant_BoolValue#,
			// LoopCondition
			@not_strictly_false(
				@result^#11:*expr.Expr_IdentExpr#
			)^#12:*expr.Expr_CallExpr#,
			// LoopStep
			_&&_(
				@result^#13:*expr.Expr_IdentExpr#,
				_>_(
					x^#7:*expr.Expr_IdentExpr#,
					0^#9:*expr.Constant_Int64Value#
				)^#8:*expr.Expr_CallExpr#
			)^#14:*expr.Expr_CallExpr#,
			// Result
			@result^#15:*expr.Expr_IdentExpr#)^#16:all#`,
	},
	{
		I: `[1, 2, 3].exists(x, x == 2)`,
		P: `__comprehension__(
			// Variable
			x,
			// Target
			[
				1^#2:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#,
				3^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			// Accumulator
			@result,
			// Init
			false^#10:*expr.Constant_BoolValue#,
			// LoopCondition
			@not_strictly_false(
				!_(
					@result^#11:*expr.Expr_IdentExpr#
				)^#12:*expr.Expr_CallExpr#
			)^#13:*expr.Expr_CallExpr#,
			// LoopStep
			_||_(
				@result^#14:*expr.Expr_IdentExpr#,
				_==_(
					x^#7:*expr.Expr_IdentExpr#,
					2^#9:*expr.Constant_Int64Value#
				)^#8:*expr.Expr_CallExpr#
			)^#15:*expr.Expr_CallExpr#,
			// Result
			@result^#16:*expr.Expr_IdentExpr#)^#17:exists#`,
	},
	{
		I: `[1, 2, 3].exists_one(x, x == 2)`,
		P: `__comprehension__(
			// Variable
			x,
			// Target
			[
				1^#2:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#,
				3^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			// Accumulator
			@result,
			// Init
			0^#10:*expr.Constant_Int64Value#,
			// LoopCondition
			true^#11:*expr.Constant_BoolValue#,
			// LoopStep
			_?_:_(
				_==_(
					x^#7:*expr.Expr_IdentExpr#,
					2^#9:*expr.Constant_Int64Value#
				)^#8:*expr.Expr_CallExpr#,
				_+_(
					@result^#12:*expr.Expr_IdentExpr#,
					1^#13:*expr.Constant_Int64Value#
				)^#14:*expr.Expr_CallExpr#,
				@result^#15:*expr.Expr_IdentExpr#
			)^#16:*expr.Expr_CallExpr#,
			// Result
			_==_(
				@result^#17:*expr.Expr_IdentExpr#,
				1^#18:*expr.Constant_Int64Value#
			)^#19:*expr.Expr_CallExpr#)^#20:exists_one#`,
	},
	{
		I: `[1, 2, 3].map(x, x * 2)`,
		P: `__comprehension__(
			// Variable
			x,
			// Target
			[
				1^#2:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#,
				3^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			// Accumulator
			@result,
			// Init
			[]^#10:*expr.Expr_ListExpr#,
			// LoopCondition
			true^#11:*expr.Constant_BoolValue#,
			// LoopStep
			_+_(
				@result^#12:*expr.Expr_IdentExpr#,
				[
					_*_(
						x^#7:*expr.Expr_IdentExpr#,
						2^#9:*expr.Constant_Int64Value#
					)^#8:*expr.Expr_CallExpr#
				]^#13:*expr.Expr_ListExpr#
			)^#14:*expr.Expr_CallExpr#,
			// Result
			@result^#15:*expr.Expr_IdentExpr#)^#16:map#`,
	},
	{
		I: `[1, 2, 3].filter(x, x > 1)`,
		P: `__comprehension__(
			// Variable
			x,
			// Target
			[
				1^#2:*expr.Constant_Int64Value#,
				2^#3:*expr.Constant_Int64Value#,
				3^#4:*expr.Constant_Int64Value#
			]^#1:*expr.Expr_ListExpr#,
			// Accumulator
			@result,
			// Init
			[]^#10:*expr.Expr_ListExpr#,
			// LoopCondition
			true^#11:*expr.Constant_BoolValue#,
			// LoopStep
			_?_:_(
				_>_(
					x^#7:*expr.Expr_IdentExpr#,
					1^#9:*expr.Constant_Int64Value#
				)^#8:*expr.Expr_CallExpr#,
				_+_(
					@result^#12:*expr.Expr_IdentExpr#,
					[
						x^#6:*expr.Expr_IdentExpr#
					]^#13:*expr.Expr_ListExpr#
				)^#14:*expr.Expr_CallExpr#,
					@result^#15:*expr.Expr_IdentExpr#
			)^#16:*expr.Expr_CallExpr#,
			// Result
			@result^#17:*expr.Expr_IdentExpr#)^#18:filter#`,
	},

	// Optional Syntax
	{
		I: `a.?b`,
		P: `_?._(
			a^#1:*expr.Expr_IdentExpr#,
			"b"^#3:*expr.Constant_StringValue#
		)^#2:*expr.Expr_CallExpr#`,
		Opts: []Option{EnableOptionalSyntax(true)},
	},
	{
		I: `a[?0]`,
		P: `_[?_](
			a^#1:*expr.Expr_IdentExpr#,
			0^#3:*expr.Constant_Int64Value#
		)^#2:*expr.Expr_CallExpr#`,
		Opts: []Option{EnableOptionalSyntax(true)},
	},
	{
		I: `[?a, b, ?c]`,
		P: `[
			a^#2:*expr.Expr_IdentExpr#,
			b^#3:*expr.Expr_IdentExpr#,
			c^#4:*expr.Expr_IdentExpr#
		]^#1:*expr.Expr_ListExpr#`,
		Opts: []Option{EnableOptionalSyntax(true)},
	},
	{
		I: `{?a: 1, b: 2}`,
		P: `{
			?a^#2:*expr.Expr_IdentExpr#:1^#4:*expr.Constant_Int64Value#^#3:*expr.Expr_CreateStruct_Entry#,
			b^#5:*expr.Expr_IdentExpr#:2^#7:*expr.Constant_Int64Value#^#6:*expr.Expr_CreateStruct_Entry#
		}^#1:*expr.Expr_StructExpr#`,
		Opts: []Option{EnableOptionalSyntax(true)},
	},
	{
		I: `pkg.Msg{?field: 42}`,
		P: `pkg.Msg{
			?field:42^#3:*expr.Constant_Int64Value#^#2:*expr.Expr_CreateStruct_Entry#
		}^#2:*expr.Expr_StructExpr#`,
		Opts: []Option{EnableOptionalSyntax(true)},
	},

	// Escaped Identifier Syntax
	{
		I:    "msg.`field-name`",
		P:    `msg^#1:*expr.Expr_IdentExpr#.field-name^#2:*expr.Expr_SelectExpr#`,
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I:    "msg.`a.b.c`",
		P:    `msg^#1:*expr.Expr_IdentExpr#.a.b.c^#2:*expr.Expr_SelectExpr#`,
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I:    "msg.`field name`",
		P:    `msg^#1:*expr.Expr_IdentExpr#.field name^#2:*expr.Expr_SelectExpr#`,
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "Msg{`field-1`: 42}",
		P: `Msg{
			field-1:42^#2:*expr.Constant_Int64Value#^#1:*expr.Expr_CreateStruct_Entry#
		}^#1:*expr.Expr_StructExpr#`,
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},

	// Error Cases
	{
		I: "(1 + 2",
		E: "ERROR: <input>:1:7: expected ')'\n" +
			" | (1 + 2\n" +
			" | ......^",
	},
	{
		I: "[1, 2",
		E: "ERROR: <input>:1:6: expected ']'\n" +
			" | [1, 2\n" +
			" | .....^",
	},
	{
		I: "{1: 2",
		E: "ERROR: <input>:1:6: expected '}'\n" +
			" | {1: 2\n" +
			" | .....^",
	},
	{
		I: "a ? b",
		E: "ERROR: <input>:1:6: expected ':' in conditional expression\n" +
			" | a ? b\n" +
			" | .....^",
	},
	{
		I: "1 + 2 3",
		E: "ERROR: <input>:1:7: Syntax error: mismatched input '3' expecting <EOF>\n" +
			" | 1 + 2 3\n" +
			" | ......^",
	},
	{
		I: "0xFFFFFFFFFFFFFFFFF",
		E: "ERROR: <input>:1:1: invalid int literal\n" +
			" | 0xFFFFFFFFFFFFFFFFF\n" +
			" | ^",
	},
	{
		I: "0xFFFFFFFFFFFFFFFFFu",
		E: "ERROR: <input>:1:1: invalid uint literal\n" +
			" | 0xFFFFFFFFFFFFFFFFFu\n" +
			" | ^",
	},
	{
		I: "1.99e90000009",
		E: "ERROR: <input>:1:1: invalid double literal\n" +
			" | 1.99e90000009\n" +
			" | ^",
	},
	{
		I: "as",
		E: "ERROR: <input>:1:1: reserved identifier: as\n" +
			" | as\n" +
			" | ^",
	},
	{
		I: "msg.`ident`",
		E: "ERROR: <input>:1:5: unsupported syntax '`'\n" +
			" | msg.`ident`\n" +
			" | ....^",
		Opts: []Option{EnableIdentEscapeSyntax(false)},
	},
	{
		I: "a.?b",
		E: "ERROR: <input>:1:2: unsupported syntax '.?'\n" +
			" | a.?b\n" +
			" | .^",
	},
	{
		I: "has(m)",
		E: "ERROR: <input>:1:5: invalid argument to has() macro\n" +
			" | has(m)\n" +
			" | ....^",
		Opts: []Option{Macros(AllMacros...)},
	},
	{
		I: "[1, 2].all(1 + 2, true)",
		E: "ERROR: <input>:1:14: argument must be a simple name\n" +
			" | [1, 2].all(1 + 2, true)\n" +
			" | .............^",
		Opts: []Option{Macros(AllMacros...)},
	},
	{
		I: "[1, 2].all(__result__, true)",
		E: "ERROR: <input>:1:12: iteration variable overwrites accumulator variable\n" +
			" | [1, 2].all(__result__, true)\n" +
			" | ...........^",
		Opts: []Option{Macros(AllMacros...)},
	},
	{
		I: "1{}",
		E: "ERROR: <input>:1:2: Syntax error: mismatched input '{' expecting <EOF>\n" +
			" | 1{}\n" +
			" | .^",
	},
	{
		I: "a.",
		E: "ERROR: <input>:1:3: expected identifier after '.'\n" +
			" | a.\n" +
			" | ..^",
	},
	{
		I: ". *",
		E: "ERROR: <input>:1:3: expected identifier\n" +
			" | . *\n" +
			" | ..^",
	},
	{
		I: ".as",
		E: "ERROR: <input>:1:2: reserved identifier: as\n" +
			" | .as\n" +
			" | .^",
	},
	{
		I: "* 2",
		E: "ERROR: <input>:1:1: unexpected token\n" +
			" | * 2\n" +
			" | ^\n" +
			"ERROR: <input>:1:3: Syntax error: mismatched input '2' expecting <EOF>\n" +
			" | * 2\n" +
			" | ..^",
	},
	{
		I: "{'k' 'v'}",
		E: "ERROR: <input>:1:6: expected ':' in map entry\n" +
			" | {'k' 'v'}\n" +
			" | .....^",
	},
	{
		I: "Msg{1: 2}",
		E: "ERROR: <input>:1:5: expected struct field name\n" +
			" | Msg{1: 2}\n" +
			" | ....^",
	},
	{
		I: "Msg{f 10}",
		E: "ERROR: <input>:1:7: expected ':' in struct field\n" +
			" | Msg{f 10}\n" +
			" | ......^",
	},
	{
		I: "Msg{f: 10",
		E: "ERROR: <input>:1:10: expected '}'\n" +
			" | Msg{f: 10\n" +
			" | .........^",
	},
	{
		I: "f(1, 2",
		E: "ERROR: <input>:1:7: Syntax error: mismatched input <EOF> expecting ')'\n" +
			" | f(1, 2\n" +
			" | ......^",
	},
	{
		I: "1e",
		E: "ERROR: <input>:1:1: floating point literal missing digits after exponent separator\n" +
			" | 1e\n" +
			" | ^",
	},
	{
		I: "\"unterminated",
		E: "ERROR: <input>:1:1: unterminated string literal\n" +
			" | \"unterminated\n" +
			" | ^",
	},
	{
		I: "b\"unterminated",
		E: "ERROR: <input>:1:1: unterminated bytes literal\n" +
			" | b\"unterminated\n" +
			" | ^",
	},
	{
		I: "a.`foo`()",
		E: "ERROR: <input>:1:3: unexpected quoted identifier\n" +
			" | a.`foo`()\n" +
			" | ..^",
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "`foo`",
		E: "ERROR: <input>:1:1: unexpected quoted identifier\n" +
			" | `foo`\n" +
			" | ^",
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "a.`b@c`",
		E: "ERROR: <input>:1:3: unexpected quoted identifier\n" +
			" | a.`b@c`\n" +
			" | ..^",
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "a.``",
		E: "ERROR: <input>:1:3: unexpected quoted identifier\n" +
			" | a.``\n" +
			" | ..^",
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "`foo",
		E: "ERROR: <input>:1:1: unterminated quoted identifier\n" +
			" | `foo\n" +
			" | ^",
		Opts: []Option{EnableIdentEscapeSyntax(true)},
	},
	{
		I: "-0x8000000000000001",
		E: "ERROR: <input>:1:2: invalid int literal\n" +
			" | -0x8000000000000001\n" +
			" | .^",
	},
	{
		I: "-9223372036854775809",
		E: "ERROR: <input>:1:2: invalid int literal\n" +
			" | -9223372036854775809\n" +
			" | .^",
	},
	{
		I: "a[?0]",
		E: "ERROR: <input>:1:2: unsupported syntax '?'\n" +
			" | a[?0]\n" +
			" | .^",
		Opts: []Option{EnableOptionalSyntax(false)},
	},
	{
		I: "[?1]",
		E: "ERROR: <input>:1:2: unsupported syntax '?'\n" +
			" | [?1]\n" +
			" | .^",
		Opts: []Option{EnableOptionalSyntax(false)},
	},
	{
		I: "{?'k': 'v'}",
		E: "ERROR: <input>:1:2: unsupported syntax '?'\n" +
			" | {?'k': 'v'}\n" +
			" | .^",
		Opts: []Option{EnableOptionalSyntax(false)},
	},
	{
		I: "Msg{?f: 1}",
		E: "ERROR: <input>:1:5: unsupported syntax '?'\n" +
			" | Msg{?f: 1}\n" +
			" | ....^",
		Opts: []Option{EnableOptionalSyntax(false)},
	},
	{
		I: "a.?`foo`",
		E: "ERROR: <input>:1:4: unsupported syntax '`'\n" +
			" | a.?`foo`\n" +
			" | ...^",
		Opts: []Option{EnableOptionalSyntax(true), EnableIdentEscapeSyntax(false)},
	},
}

func parse(source common.Source, opts ...Option) (*ast.AST, *common.Errors) {
	p, err := NewPrattParser(opts...)
	if err != nil {
		panic(err)
	}
	return p.Parse(source)
}

func TestPrattParser(t *testing.T) {
	for i, tst := range prattTestCases {
		name := fmt.Sprintf("%d %s", i, tst.I)
		// Local variable required as the closure will reference the value for the last
		// 'tst' value rather than the local 'tc' instance declared within the loop.
		tc := tst
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := tc.Opts
			if len(opts) == 0 {
				opts = []Option{Macros(AllMacros...), PopulateMacroCalls(true)}
			}
			src := common.NewTextSource(tc.I)
			parsed, errors := parse(src, opts...)
			if len(errors.GetErrors()) > 0 {
				actualErr := errors.ToDisplayString()
				if tc.E == "" {
					t.Fatalf("Unexpected errors: %v", actualErr)
				} else if !test.Compare(actualErr, tc.E) {
					t.Fatal(test.DiffMessage("Error mismatch", actualErr, tc.E))
				}
				return
			} else if tc.E != "" {
				t.Fatalf("Expected error not thrown: '%s'", tc.E)
			}
			failureDisplayMethod := fmt.Sprintf("Parse(\"%s\")", tc.I)
			actualWithKind := debug.ToAdornedDebugString(parsed.Expr(), &kindAndIDAdorner{parsed.SourceInfo()})
			if !test.Compare(actualWithKind, tc.P) {
				t.Fatal(test.DiffMessage(fmt.Sprintf("Structure - %s", failureDisplayMethod), actualWithKind, tc.P))
			}
		})
	}
}
func TestPrattParserSourceInfoPositions(t *testing.T) {
	src := common.NewTextSource("a + b")
	p, err := NewPrattParser()
	if err != nil {
		t.Fatalf("NewPrattParser() failed: %v", err)
	}
	parsed, errs := p.Parse(src)
	if len(errs.GetErrors()) > 0 {
		t.Fatalf("Parse() failed: %s", errs.ToDisplayString())
	}
	sourceInfo := parsed.SourceInfo()
	root := parsed.Expr()
	if sourceInfo.GetStartLocation(root.ID()).Column() != 2 {
		t.Errorf("expected root column 2, got %d", sourceInfo.GetStartLocation(root.ID()).Column())
	}
	args := root.AsCall().Args()
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if sourceInfo.GetStartLocation(args[0].ID()).Column() != 0 {
		t.Errorf("expected arg[0] column 0, got %d", sourceInfo.GetStartLocation(args[0].ID()).Column())
	}
	if sourceInfo.GetStartLocation(args[1].ID()).Column() != 4 {
		t.Errorf("expected arg[1] column 4, got %d", sourceInfo.GetStartLocation(args[1].ID()).Column())
	}
}

func TestPrattParserRecursionDepth(t *testing.T) {
	t.Run("DeeplyNestedBracketsLimitExceeded", func(t *testing.T) {
		p, err := NewPrattParser(MaxRecursionDepth(5))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		_, errs := p.Parse(common.NewTextSource("[[[[[[1]]]]]]"))
		if len(errs.GetErrors()) == 0 {
			t.Errorf("expected recursion limit error, got none")
		}
	})

	t.Run("IgnoreExtraParens", func(t *testing.T) {
		p, err := NewPrattParser(MaxRecursionDepth(1))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		_, errs := p.Parse(common.NewTextSource("((((1))))"))
		if len(errs.GetErrors()) > 0 {
			t.Errorf("unexpected error: %s", errs.ToDisplayString())
		}
	})

	t.Run("DeeplyNestedParens1000", func(t *testing.T) {
		p, err := NewPrattParser(MaxRecursionDepth(1))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		expr1 := strings.Repeat("(", 1000) + "42" + strings.Repeat(")", 1000)
		_, errs := p.Parse(common.NewTextSource(expr1))
		if len(errs.GetErrors()) > 0 {
			t.Errorf("unexpected error on 1000 parens literal: %s", errs.ToDisplayString())
		}

		expr2 := strings.Repeat("(", 1000) + "1 + 2" + strings.Repeat(")", 1000)
		_, errs = p.Parse(common.NewTextSource(expr2))
		if len(errs.GetErrors()) > 0 {
			t.Errorf("unexpected error on 1000 parens binary: %s", errs.ToDisplayString())
		}
	})

	t.Run("SequentialScopesDoNotAccumulateDepth", func(t *testing.T) {
		p, err := NewPrattParser(MaxRecursionDepth(2))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		_, errs := p.Parse(common.NewTextSource("[1] + [2] + [3]"))
		if len(errs.GetErrors()) > 0 {
			t.Errorf("unexpected error on sequential scopes: %s", errs.ToDisplayString())
		}
	})
}

func TestPrattParserMacroCalls(t *testing.T) {
	t.Run("DisabledByDefault", func(t *testing.T) {
		p, err := NewPrattParser(Macros(AllMacros...), PopulateMacroCalls(false))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		parsed, errs := p.Parse(common.NewTextSource("has(a.b)"))
		if len(errs.GetErrors()) > 0 {
			t.Fatalf("unexpected error: %s", errs.ToDisplayString())
		}
		if len(parsed.SourceInfo().MacroCalls()) != 0 {
			t.Errorf("expected 0 macro calls, got %d", len(parsed.SourceInfo().MacroCalls()))
		}
	})

	t.Run("GlobalMacroCallRecorded", func(t *testing.T) {
		p, err := NewPrattParser(Macros(AllMacros...), PopulateMacroCalls(true))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		parsed, errs := p.Parse(common.NewTextSource("has(a.b)"))
		if len(errs.GetErrors()) > 0 {
			t.Fatalf("unexpected error: %s", errs.ToDisplayString())
		}
		macroCalls := parsed.SourceInfo().MacroCalls()
		if len(macroCalls) != 1 {
			t.Fatalf("expected 1 macro call, got %d", len(macroCalls))
		}
	})

	t.Run("ReceiverMacroCallRecorded", func(t *testing.T) {
		p, err := NewPrattParser(Macros(AllMacros...), PopulateMacroCalls(true))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		parsed, errs := p.Parse(common.NewTextSource("[1, 2].exists(x, x > 0)"))
		if len(errs.GetErrors()) > 0 {
			t.Fatalf("unexpected error: %s", errs.ToDisplayString())
		}
		macroCalls := parsed.SourceInfo().MacroCalls()
		if len(macroCalls) != 1 {
			t.Fatalf("expected 1 macro call, got %d", len(macroCalls))
		}
	})

	t.Run("NestedMacroCallsRecorded", func(t *testing.T) {
		p, err := NewPrattParser(Macros(AllMacros...), PopulateMacroCalls(true))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		parsed, errs := p.Parse(common.NewTextSource("[1, 2].all(x, has(x.b))"))
		if len(errs.GetErrors()) > 0 {
			t.Fatalf("unexpected error: %s", errs.ToDisplayString())
		}
		macroCalls := parsed.SourceInfo().MacroCalls()
		if len(macroCalls) != 2 {
			t.Fatalf("expected 2 macro calls, got %d", len(macroCalls))
		}
	})
}

func TestPrattParserErrorRecoveryLimits(t *testing.T) {
	t.Run("LimitZero", func(t *testing.T) {
		p, err := NewPrattParser(ErrorRecoveryLimit(0))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		_, errs := p.Parse(common.NewTextSource("......"))
		if len(errs.GetErrors()) == 0 {
			t.Errorf("expected error recovery limit error, got none")
		}
	})

	t.Run("LimitOne", func(t *testing.T) {
		p, err := NewPrattParser(ErrorRecoveryLimit(1))
		if err != nil {
			t.Fatalf("NewPrattParser() failed: %v", err)
		}
		_, errs := p.Parse(common.NewTextSource("......"))
		if len(errs.GetErrors()) == 0 {
			t.Errorf("expected error recovery limit error, got none")
		}
	})
}

func TestPrattParserExpressionSizeCodePointLimit(t *testing.T) {
	p, err := NewPrattParser(Macros(AllMacros...), ExpressionSizeCodePointLimit(2))
	if err != nil {
		t.Fatal(err)
	}
	src := common.NewTextSource("foo")
	_, errs := p.Parse(src)
	if got, want := len(errs.GetErrors()), 1; got != want {
		t.Fatalf("got %d errors, want %d errors: %s", got, want, errs.ToDisplayString())
	}
	if got, want := errs.GetErrors()[0].Message, "expression code point size exceeds limit: size: 3, limit 2"; got != want {
		t.Fatalf("got %q, want %q: %s", got, want, errs.GetErrors()[0].ToDisplayString(src))
	}
}

func BenchmarkParsers(b *testing.B) {
	exprs := []string{
		`42`,
		`a > 5 && b < 10 || c == "xyz"`,
		`[1, 2, 3].all(x, x > 0) && [4, 5, 6].exists(y, y == 5)`,
		`pkg.Msg{field1: "value", field2: 123, list_field: [1, 2, 3], map_field: {"a": true, "b": false}}`,
		`a.b.c.d.e.f(1, 2, [3, ?4], {?5: 6}) ? (x + y * z - w / v) : (!p && !q || r.s)`,
	}

	antlrParser, _ := NewParser(Macros(AllMacros...), PopulateMacroCalls(true), EnableOptionalSyntax(true))
	prattParser, _ := NewPrattParser(Macros(AllMacros...), PopulateMacroCalls(true), EnableOptionalSyntax(true))

	for _, expr := range exprs {
		src := common.NewTextSource(expr)

		b.Run("ANTLR/"+expr[:min(len(expr), 20)], func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = antlrParser.Parse(src)
			}
		})

		b.Run("Pratt/"+expr[:min(len(expr), 20)], func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = prattParser.Parse(src)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
