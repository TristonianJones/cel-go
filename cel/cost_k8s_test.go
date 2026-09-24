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
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/ext"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestK8sSetsCost(t *testing.T) {
	cases := []struct {
		name                string
		expr                string
		expectEstimatedCost cost.CostEstimate
		expectRuntimeCost   uint64
	}{
		{
			name:                "sets",
			expr:                `sets.contains([], [])`,
			expectEstimatedCost: cost.CostEstimate{Min: 21, Max: 21},
			expectRuntimeCost:   21,
		},
		{
			expr:                `sets.contains([1], [])`,
			expectEstimatedCost: cost.CostEstimate{Min: 21, Max: 21},
			expectRuntimeCost:   21,
		},
		{
			expr:                `sets.contains([1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 22, Max: 22},
			expectRuntimeCost:   22,
		},
		{
			expr:                `sets.contains([1], [1, 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.contains([1, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.contains([2, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.contains([1, 2, 3, 4], [2, 3])`,
			expectEstimatedCost: cost.CostEstimate{Min: 29, Max: 29},
			expectRuntimeCost:   29,
		},
		{
			expr:                `sets.contains([1], [1.0, 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.contains([1, 2], [2u, 2.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.contains([1, 2u], [2, 2.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.contains([1, 2.0, 3u], [1.0, 2u, 3])`,
			expectEstimatedCost: cost.CostEstimate{Min: 30, Max: 30},
			expectRuntimeCost:   30,
		},
		{
			expr: `sets.contains([[1], [2, 3]], [[2, 3.0]])`,
			// 10 for each list creation, top-level list sizes are 2, 1
			expectEstimatedCost: cost.CostEstimate{Min: 53, Max: 53},
			expectRuntimeCost:   53,
		},
		{
			expr:                `!sets.contains([1], [2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `!sets.contains([1], [1, 2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 24, Max: 24},
			expectRuntimeCost:   24,
		},
		{
			expr:                `!sets.contains([1], ["1", 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 24, Max: 24},
			expectRuntimeCost:   24,
		},
		{
			expr:                `!sets.contains([1], [1.1, 1u])`,
			expectEstimatedCost: cost.CostEstimate{Min: 24, Max: 24},
			expectRuntimeCost:   24,
		},

		// set equivalence (note the cost factor is higher as it's basically two contains checks)
		{
			expr:                `sets.equivalent([], [])`,
			expectEstimatedCost: cost.CostEstimate{Min: 21, Max: 21},
			expectRuntimeCost:   21,
		},
		{
			expr:                `sets.equivalent([1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.equivalent([1], [1, 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.equivalent([1, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.equivalent([1], [1u, 1.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.equivalent([1], [1u, 1.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 25, Max: 25},
			expectRuntimeCost:   25,
		},
		{
			expr:                `sets.equivalent([1, 2, 3], [3u, 2.0, 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 39, Max: 39},
			expectRuntimeCost:   39,
		},
		{
			expr:                `sets.equivalent([[1.0], [2, 3]], [[1], [2, 3.0]])`,
			expectEstimatedCost: cost.CostEstimate{Min: 69, Max: 69},
			expectRuntimeCost:   69,
		},
		{
			expr:                `!sets.equivalent([2, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 26, Max: 26},
			expectRuntimeCost:   26,
		},
		{
			expr:                `!sets.equivalent([1], [1, 2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 26, Max: 26},
			expectRuntimeCost:   26,
		},
		{
			expr:                `!sets.equivalent([1, 2], [2u, 2, 2.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 34, Max: 34},
			expectRuntimeCost:   34,
		},
		{
			expr:                `!sets.equivalent([1, 2], [1u, 2, 2.3])`,
			expectEstimatedCost: cost.CostEstimate{Min: 34, Max: 34},
			expectRuntimeCost:   34,
		},
		{
			expr:                `sets.intersects([1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 22, Max: 22},
			expectRuntimeCost:   22,
		},
		{
			expr:                `sets.intersects([1], [1, 1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.intersects([1, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.intersects([2, 1], [1])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.intersects([1], [1, 2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.intersects([1], [1.0, 2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `sets.intersects([1, 2], [2u, 2, 2.0])`,
			expectEstimatedCost: cost.CostEstimate{Min: 27, Max: 27},
			expectRuntimeCost:   27,
		},
		{
			expr:                `sets.intersects([1, 2], [1u, 2, 2.3])`,
			expectEstimatedCost: cost.CostEstimate{Min: 27, Max: 27},
			expectRuntimeCost:   27,
		},
		{
			expr:                `sets.intersects([[1], [2, 3]], [[1, 2], [2, 3.0]])`,
			expectEstimatedCost: cost.CostEstimate{Min: 65, Max: 65},
			expectRuntimeCost:   65,
		},
		{
			expr:                `!sets.intersects([], [])`,
			expectEstimatedCost: cost.CostEstimate{Min: 22, Max: 22},
			expectRuntimeCost:   22,
		},
		{
			expr:                `!sets.intersects([1], [])`,
			expectEstimatedCost: cost.CostEstimate{Min: 22, Max: 22},
			expectRuntimeCost:   22,
		},
		{
			expr:                `!sets.intersects([1], [2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 23, Max: 23},
			expectRuntimeCost:   23,
		},
		{
			expr:                `!sets.intersects([1], ["1", 2])`,
			expectEstimatedCost: cost.CostEstimate{Min: 24, Max: 24},
			expectRuntimeCost:   24,
		},
		{
			expr:                `!sets.intersects([1], [1.1, 2u])`,
			expectEstimatedCost: cost.CostEstimate{Min: 24, Max: 24},
			expectRuntimeCost:   24,
		},
	}

	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			testK8sCost(t, tc.expr, tc.expectEstimatedCost, tc.expectRuntimeCost)
		})
	}
}

func TestK8sTwoVariableComprehensionCost(t *testing.T) {
	cases := []struct {
		name                string
		expr                string
		expectEstimatedCost cost.CostEstimate
		expectRuntimeCost   uint64
	}{
		{
			name:                "map all",
			expr:                `{'a': 1, 'b': 2}.all(k, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 37, Max: 41},
			expectRuntimeCost:   41,
		},
		{
			name:                "map exists",
			expr:                `{'a': 1, 'b': 2}.exists(k, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 39, Max: 43},
			expectRuntimeCost:   40,
		},
		{
			name:                "map existsOne",
			expr:                `{'a': 1, 'b': 2}.existsOne(k, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 38, Max: 40},
			expectRuntimeCost:   40,
		},
		{
			name:                "map transformMap",
			expr:                `{'a': 1, 'b': 2}.transformMap(k, v, v + 1)`,
			expectEstimatedCost: cost.CostEstimate{Min: 71, Max: 71},
			expectRuntimeCost:   71,
		},
		{
			name:                "map transformMap with filter",
			expr:                `{'a': 1, 'b': 2}.transformMap(k, v, v < 5, v + 1)`,
			expectEstimatedCost: cost.CostEstimate{Min: 67, Max: 75},
			expectRuntimeCost:   75,
		},
		{
			name:                "map transformMapEntry",
			expr:                `{'a': 1, 'b': 2}.transformMapEntry(k, v, {k: v + 1})`,
			expectEstimatedCost: cost.CostEstimate{Min: 131, Max: 131},
			expectRuntimeCost:   131,
		},
		{
			name:                "map transformMapEntry with filter",
			expr:                `{'a': 1, 'b': 2}.transformMapEntry(k, v, v < 5, {k: v + 1})`,
			expectEstimatedCost: cost.CostEstimate{Min: 67, Max: 135},
			expectRuntimeCost:   135,
		},

		{
			name:                "list all",
			expr:                `[1, 2].all(i, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 17, Max: 21},
			expectRuntimeCost:   21,
		},
		{
			name:                "list exists",
			expr:                `[1, 2].exists(i, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 19, Max: 23},
			expectRuntimeCost:   20,
		},
		{
			name:                "list existsOne",
			expr:                `[1, 2].existsOne(i, v, v > 0)`,
			expectEstimatedCost: cost.CostEstimate{Min: 18, Max: 20},
			expectRuntimeCost:   20,
		},
		{
			name:                "list transformList",
			expr:                `[1, 2].transformList(i, v, v + 1)`,
			expectEstimatedCost: cost.CostEstimate{Min: 49, Max: 49},
			expectRuntimeCost:   49,
		},
		{
			name:                "list transformList with filter",
			expr:                `[1, 2].transformList(i, v, v < 5, v + 1)`,
			expectEstimatedCost: cost.CostEstimate{Min: 27, Max: 53},
			expectRuntimeCost:   53,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testK8sCost(t, tc.expr, tc.expectEstimatedCost, tc.expectRuntimeCost)
		})
	}
}

func TestK8sStringQuoteCost(t *testing.T) {
	cases := []struct {
		name                string
		expr                string
		expectEstimatedCost cost.CostEstimate
		expectRuntimeCost   uint64
	}{
		{
			name:                "quote",
			expr:                "strings.quote('ABCDEFGHIJ abcdefghij')",
			expectEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			expectRuntimeCost:   3,
		},
		{
			name:                "quoteEquals",
			expr:                "strings.quote('ABCDEFGHIJ abcdefghij') == strings.quote('ABCDEFGHIJ abcdefghij')",
			expectEstimatedCost: cost.CostEstimate{Min: 7, Max: 11},
			expectRuntimeCost:   9,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testK8sCost(t, tc.expr, tc.expectEstimatedCost, tc.expectRuntimeCost)
		})
	}
}

func TestK8sIPCost(t *testing.T) {
	ipv4 := "ip('192.168.0.1')"
	ipv4BaseEstimatedCost := cost.CostEstimate{Min: 2, Max: 2}
	ipv4BaseRuntimeCost := uint64(2)

	ipv6 := "ip('2001:db8:3333:4444:5555:6666:7777:8888')"
	ipv6BaseEstimatedCost := cost.CostEstimate{Min: 4, Max: 4}
	ipv6BaseRuntimeCost := uint64(4)

	testCases := []struct {
		ops                 []string
		expectEstimatedCost func(cost.CostEstimate) cost.CostEstimate
		expectRuntimeCost   func(uint64) uint64
	}{
		{
			// For just parsing the IP, the cost is expected to be the base.
			ops:                 []string{""},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate { return c },
			expectRuntimeCost:   func(c uint64) uint64 { return c },
		},
		{
			ops: []string{".family()", ".isUnspecified()", ".isLoopback()", ".isLinkLocalMulticast()", ".isLinkLocalUnicast()", ".isGlobalUnicast()"},
			// For most other operations, the cost is expected to be the base + 1.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 1, Max: c.Max + 1}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 1 },
		},
		{
			ops: []string{" == ip('192.168.0.1')"},
			// In CEL-go, equality for opaque types is estimated as [1, 2].
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return c.Add(ipv4BaseEstimatedCost).Add(cost.CostEstimate{Min: 1, Max: 2})
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + ipv4BaseRuntimeCost + 1 },
		},
	}

	for _, tc := range testCases {
		for _, op := range tc.ops {
			t.Run(ipv4+op, func(t *testing.T) {
				testK8sCost(t, ipv4+op, tc.expectEstimatedCost(ipv4BaseEstimatedCost), tc.expectRuntimeCost(ipv4BaseRuntimeCost))
			})

			t.Run(ipv6+op, func(t *testing.T) {
				testK8sCost(t, ipv6+op, tc.expectEstimatedCost(ipv6BaseEstimatedCost), tc.expectRuntimeCost(ipv6BaseRuntimeCost))
			})
		}
	}
}

func TestK8sIPIsCanonicalCost(t *testing.T) {
	testCases := []struct {
		op                  string
		expectEstimatedCost cost.CostEstimate
		expectRuntimeCost   uint64
	}{
		{
			op:                  "ip.isCanonical('192.168.0.1')",
			expectEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			expectRuntimeCost:   3,
		},
		{
			op:                  "ip.isCanonical('2001:db8:3333:4444:5555:6666:7777:8888')",
			expectEstimatedCost: cost.CostEstimate{Min: 8, Max: 8},
			expectRuntimeCost:   8,
		},
		{
			op:                  "ip.isCanonical('2001:db8::abcd')",
			expectEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			expectRuntimeCost:   3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.op, func(t *testing.T) {
			testK8sCost(t, tc.op, tc.expectEstimatedCost, tc.expectRuntimeCost)
		})
	}
}

func TestK8sCIDRCost(t *testing.T) {
	ipv4 := "cidr('192.168.0.0/16')"
	ipv4BaseEstimatedCost := cost.CostEstimate{Min: 2, Max: 2}
	ipv4BaseRuntimeCost := uint64(2)

	ipv6 := "cidr('2001:db8::/32')"
	ipv6BaseEstimatedCost := cost.CostEstimate{Min: 2, Max: 2}
	ipv6BaseRuntimeCost := uint64(2)

	type testCase struct {
		ops                 []string
		expectEstimatedCost func(cost.CostEstimate) cost.CostEstimate
		expectRuntimeCost   func(uint64) uint64
	}

	cases := []testCase{
		{
			// For just parsing the IP, the cost is expected to be the base.
			ops:                 []string{""},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate { return c },
			expectRuntimeCost:   func(c uint64) uint64 { return c },
		},
		{
			ops: []string{".ip()", ".prefixLength()", ".masked()"},
			// For most other operations, the cost is expected to be the base + 1.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 1, Max: c.Max + 1}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 1 },
		},
		{
			ops: []string{" == cidr('2001:db8::/32')"},
			// In CEL-go, equality for opaque types is estimated as [1, 2].
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return c.Add(ipv6BaseEstimatedCost).Add(cost.CostEstimate{Min: 1, Max: 2})
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + ipv6BaseRuntimeCost + 1 },
		},
	}

	ipv4Cases := append(cases, []testCase{
		{
			ops: []string{".containsCIDR(cidr('192.0.0.0/30'))"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR(cidr('192.168.0.0/16'))"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR('192.0.0.0/30')"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR('192.168.0.0/16')"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsIP(ip('192.0.0.1'))"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 2, Max: c.Max + 5}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 2 },
		},
		{
			ops: []string{".containsIP(ip('192.169.0.1'))"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 3, Max: c.Max + 6}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 3 },
		},
		{
			ops: []string{".containsIP(ip('192.169.169.250'))"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 3, Max: c.Max + 6}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 3 },
		},
		{
			ops: []string{".containsIP('192.0.0.1')"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 2, Max: c.Max + 5}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 2 },
		},
		{
			ops: []string{".containsIP('192.169.0.1')"},
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 3, Max: c.Max + 6}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 3 },
		},
	}...)

	ipv6Cases := append(cases, []testCase{
		{
			ops: []string{".containsCIDR(cidr('2001:db8::/126'))"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR(cidr('2001:db8::/32'))"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR('2001:db8::/126')"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsCIDR('2001:db8::/32')"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 9}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsIP(ip('2001:db8:3333:4444:5555:6666:7777:8888'))"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 8}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsIP(ip('2001:db8::1'))"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 3, Max: c.Max + 6}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 3 },
		},
		{
			ops: []string{".containsIP('2001:db8:3333:4444:5555:6666:7777:8888')"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 5, Max: c.Max + 8}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 5 },
		},
		{
			ops: []string{".containsIP('2001:db8::1')"},
			// For operations like checking if an IP is in a CIDR, the cost is expected to higher.
			expectEstimatedCost: func(c cost.CostEstimate) cost.CostEstimate {
				return cost.CostEstimate{Min: c.Min + 3, Max: c.Max + 6}
			},
			expectRuntimeCost: func(c uint64) uint64 { return c + 3 },
		},
	}...)

	for _, tc := range ipv4Cases {
		for _, op := range tc.ops {
			t.Run(ipv4+op, func(t *testing.T) {
				testK8sCost(t, ipv4+op, tc.expectEstimatedCost(ipv4BaseEstimatedCost), tc.expectRuntimeCost(ipv4BaseRuntimeCost))
			})
		}
	}

	for _, tc := range ipv6Cases {
		for _, op := range tc.ops {
			t.Run(ipv6+op, func(t *testing.T) {
				testK8sCost(t, ipv6+op, tc.expectEstimatedCost(ipv6BaseEstimatedCost), tc.expectRuntimeCost(ipv6BaseRuntimeCost))
			})
		}
	}
}

// TestK8sNonConformance_CostEstimators demonstrates the non-k8s conformance differences
// where Kubernetes used a custom CostEstimator (in k8s.io/apiserver/pkg/cel/library/cost.go)
// rather than CEL-go's built-in cost estimators.
func TestK8sNonConformance_CostEstimators(t *testing.T) {
	cases := []struct {
		name                   string
		expr                   string
		k8sEstimatedCost       cost.CostEstimate
		celEstimatedCost       cost.CostEstimate
		k8sRuntimeCost         uint64
		celRuntimeCost         uint64
		reason                 string
	}{
		// 1. IP and CIDR Equality (_==_)
		// Kubernetes hardcoded _==_ for IP/CIDR types to a fixed unit cost of 1 (Min: 1, Max: 1).
		// CEL-go estimates _==_ for opaque types as [1, 2].
		{
			name:             "ip_equality",
			expr:             "ip('192.168.0.1') == ip('192.168.0.1')",
			k8sEstimatedCost: cost.CostEstimate{Min: 5, Max: 5},
			celEstimatedCost: cost.CostEstimate{Min: 5, Max: 6},
			k8sRuntimeCost:   5,
			celRuntimeCost:   5,
			reason:           "K8s custom estimator hardcoded _==_ on IP/CIDR to fixed cost 1, whereas CEL-go estimates _==_ on opaque types as [1, 2]",
		},
		{
			name:             "ipv6_equality",
			expr:             "ip('2001:db8:3333:4444:5555:6666:7777:8888') == ip('192.168.0.1')",
			k8sEstimatedCost: cost.CostEstimate{Min: 7, Max: 7},
			celEstimatedCost: cost.CostEstimate{Min: 7, Max: 8},
			k8sRuntimeCost:   7,
			celRuntimeCost:   7,
			reason:           "K8s custom estimator hardcoded _==_ on IP/CIDR to fixed cost 1, whereas CEL-go estimates _==_ on opaque types as [1, 2]",
		},
		{
			name:             "cidr_equality",
			expr:             "cidr('192.168.0.0/16') == cidr('2001:db8::/32')",
			k8sEstimatedCost: cost.CostEstimate{Min: 5, Max: 5},
			celEstimatedCost: cost.CostEstimate{Min: 5, Max: 6},
			k8sRuntimeCost:   5,
			celRuntimeCost:   5,
			reason:           "K8s custom estimator hardcoded _==_ on IP/CIDR to fixed cost 1, whereas CEL-go estimates _==_ on opaque types as [1, 2]",
		},

		// 2. String Library Transformations
		// Kubernetes custom CostEstimator calculated cost purely based on ceil(len * 0.1) traversal without
		// call overhead/result size estimation. CEL-go ext.Strings uses CallCostEstimate + string scan + allocation sizing.
		{
			name:             "string_lowerAscii",
			expr:             "'ABCDEFGHIJ abcdefghij'.lowerAscii()",
			k8sEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   3,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 0.1) = 3 for lowerAscii on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_upperAscii",
			expr:             "'ABCDEFGHIJ abcdefghij'.upperAscii()",
			k8sEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   3,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 0.1) = 3 for upperAscii on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_replace",
			expr:             "'abc 123 def 123'.replace('123', '456')",
			k8sEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   3,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 2 * 0.1) = 3 for replace on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_split",
			expr:             "'abc 123 def 123'.split(' ')",
			k8sEstimatedCost: cost.CostEstimate{Min: 3, Max: 3},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   3,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 2 * 0.1) = 3 for split on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_substring",
			expr:             "'abc 123 def 123'.substring(5)",
			k8sEstimatedCost: cost.CostEstimate{Min: 2, Max: 2},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   2,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 0.1) = 2 for substring on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_trim",
			expr:             "'  abc 123 def 123  '.trim()",
			k8sEstimatedCost: cost.CostEstimate{Min: 2, Max: 2},
			celEstimatedCost: cost.CostEstimate{Min: 1, Max: 1},
			k8sRuntimeCost:   2,
			celRuntimeCost:   1,
			reason:           "K8s custom estimator used ceil(len * 0.1) = 2 for trim on string literal, whereas CEL-go uses standard literal cost",
		},
		{
			name:             "string_join",
			expr:             "['aa', 'bb', 'cc', 'd', 'e', 'f', 'g', 'h', 'i', 'j'].join(' ')",
			k8sEstimatedCost: cost.CostEstimate{Min: 11, Max: 23},
			celEstimatedCost: cost.CostEstimate{Min: 11, Max: 11},
			k8sRuntimeCost:   15,
			celRuntimeCost:   11,
			reason:           "K8s custom estimator used ceil(resultLen * 2 * 0.1) with string hints for join, whereas CEL-go computes cost directly from list size and separator",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify that CEL-go produces the celEstimatedCost and celRuntimeCost as triaged.
			testK8sCost(t, tc.expr, tc.celEstimatedCost, tc.celRuntimeCost)

			// Document that K8s expected values differ from CEL-go's actual values.
			if tc.k8sEstimatedCost != tc.celEstimatedCost {
				t.Logf("[%s] Non-conformance in estimated cost: K8s expected %v, CEL-go produces %v. Reason: %s",
					tc.name, tc.k8sEstimatedCost, tc.celEstimatedCost, tc.reason)
			}
			if tc.k8sRuntimeCost != tc.celRuntimeCost {
				t.Logf("[%s] Non-conformance in runtime cost: K8s expected %d, CEL-go produces %d. Reason: %s",
					tc.name, tc.k8sRuntimeCost, tc.celRuntimeCost, tc.reason)
			}
		})
	}
}

func testK8sCost(t *testing.T, expr string, expectEstimatedCost cost.CostEstimate, expectRuntimeCost uint64) {
	t.Helper()
	est := &k8sTestCostEstimator{}
	env, err := cel.NewEnv(
		ext.Strings(ext.StringsVersion(2)),
		ext.Lists(ext.ListsVersion(1)),
		ext.Sets(),
		ext.TwoVarComprehensions(),
		ext.Network(),
		cel.OptionalTypes(),
		cel.CostEstimatorOptions(cost.PresenceTestHasCost(false)),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	compiled, issues := env.Compile(expr)
	if issues.Err() != nil {
		t.Fatalf("env.Compile(%q) failed: %v", expr, issues.Err())
	}
	estCost, err := env.EstimateCost(compiled, est)
	if err != nil {
		t.Fatalf("env.EstimateCost() failed: %v", err)
	}
	if estCost.Min != expectEstimatedCost.Min || estCost.Max != expectEstimatedCost.Max {
		t.Errorf("Expected estimated cost of %d..%d but got %d..%d", expectEstimatedCost.Min, expectEstimatedCost.Max, estCost.Min, estCost.Max)
	}
	prog, err := env.Program(compiled, cel.CostTracking(k8sTestRuntimeCostEstimator{}))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}
	_, details, err := prog.Eval(cel.NoVars())
	if err != nil {
		t.Fatalf("prog.Eval() failed: %v", err)
	}
	actualCost := details.ActualCost()
	if actualCost == nil {
		t.Fatalf("details.ActualCost() is nil")
	}
	if *actualCost != expectRuntimeCost {
		t.Errorf("Expected runtime cost of %d but got %d", expectRuntimeCost, *actualCost)
	}
}

type k8sTestCostEstimator struct{}

func (t *k8sTestCostEstimator) EstimateSize(element cost.AstNode) *cost.SizeEstimate {
	expr, err := cel.TypeToExprType(element.Type())
	if err != nil {
		return nil
	}
	switch expr.GetPrimitive() {
	case exprpb.Type_STRING:
		return &cost.SizeEstimate{Min: 0, Max: 12}
	case exprpb.Type_BYTES:
		return &cost.SizeEstimate{Min: 0, Max: 12}
	}
	return nil
}

func (t *k8sTestCostEstimator) EstimateCallCost(function, overloadID string, target *cost.AstNode, args []cost.AstNode) *cost.CallEstimate {
	return nil
}

type k8sTestRuntimeCostEstimator struct{}

func (k8sTestRuntimeCostEstimator) CallCost(function, overloadID string, args []ref.Val, result ref.Val) *uint64 {
	return nil
}
