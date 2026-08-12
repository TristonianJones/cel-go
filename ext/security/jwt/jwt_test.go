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

package jwt_test

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext/security/jwt"
)

func createTestJWT(t *testing.T, header, payload map[string]any) string {
	hBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("json.Marshal header failed: %v", err)
	}
	pBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload failed: %v", err)
	}

	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	pB64 := base64.RawURLEncoding.EncodeToString(pBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString([]byte("signature-placeholder"))

	return hB64 + "." + pB64 + "." + sigB64
}

func evalExpr(t *testing.T, env *cel.Env, expr string, vars map[string]any) any {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Compile(%q) failed: %v", expr, issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Program(%q) failed: %v", expr, err)
	}
	val, _, err := prg.Eval(vars)
	if err != nil {
		t.Fatalf("Eval(%q) failed: %v", expr, err)
	}
	return val.Value()
}

func TestJWTParseAndPresentedBy(t *testing.T) {
	header := map[string]any{
		"alg": "RS256",
		"typ": "JWT",
		"kid": "key-123",
	}
	payload := map[string]any{
		"iss":    "https://auth.example.com",
		"sub":    "user_12345",
		"aud":    []string{"https://api.example.com", "https://admin.example.com"},
		"exp":    time.Now().Add(1 * time.Hour).Unix(),
		"nbf":    time.Now().Add(-1 * time.Minute).Unix(),
		"iat":    time.Now().Add(-1 * time.Minute).Unix(),
		"jti":    "token-unique-id-999",
		"roles":  []string{"admin", "editor"},
		"tenant": "tenant_abc",
	}

	tokenStr := createTestJWT(t, header, payload)

	env, err := cel.NewEnv(
		jwt.Library(),
		cel.Variable("tokenStr", cel.StringType),
		cel.Variable("bearerToken", cel.StringType),
		cel.Variable("upperBearerToken", cel.StringType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}

	vars := map[string]any{
		"tokenStr":         tokenStr,
		"bearerToken":      "Bearer " + tokenStr,
		"upperBearerToken": "BEARER   " + tokenStr,
	}

	tests := []struct {
		name string
		expr string
		want any
	}{
		{
			name: "parse_has_value",
			expr: `jwt.parse(tokenStr).hasValue()`,
			want: true,
		},
		{
			name: "parse_bearer_prefix_has_value",
			expr: `jwt.parse(bearerToken).hasValue()`,
			want: true,
		},
		{
			name: "parse_uppercase_bearer_prefix_has_value",
			expr: `jwt.parse(upperBearerToken).hasValue()`,
			want: true,
		},
		{
			name: "token_algorithm",
			expr: `jwt.parse(tokenStr).value().alg == 'RS256'`,
			want: true,
		},
		{
			name: "token_issuer",
			expr: `jwt.parse(tokenStr).value().issuer == 'https://auth.example.com'`,
			want: true,
		},
		{
			name: "token_subject",
			expr: `jwt.parse(tokenStr).value().subject == 'user_12345'`,
			want: true,
		},
		{
			name: "token_key_id",
			expr: `jwt.parse(tokenStr).value().keyId == 'key-123'`,
			want: true,
		},
		{
			name: "token_id",
			expr: `jwt.parse(tokenStr).value().id == 'token-unique-id-999'`,
			want: true,
		},
		{
			name: "token_audience",
			expr: `'https://api.example.com' in jwt.parse(tokenStr).value().aud`,
			want: true,
		},
		{
			name: "token_presented_by_direct",
			expr: `jwt.parse(tokenStr).value().presentedBy('https://auth.example.com', 'https://api.example.com')`,
			want: true,
		},
		{
			name: "token_presented_by_on_optional",
			expr: `jwt.parse(tokenStr).presentedBy('https://auth.example.com', 'https://api.example.com')`,
			want: true,
		},
		{
			name: "token_presented_by_mismatch_iss",
			expr: `jwt.parse(tokenStr).presentedBy('https://evil.com', 'https://api.example.com')`,
			want: false,
		},
		{
			name: "token_presented_by_mismatch_aud",
			expr: `jwt.parse(tokenStr).presentedBy('https://auth.example.com', 'https://wrong-aud.com')`,
			want: false,
		},
		{
			name: "claim_tenant",
			expr: `jwt.parse(tokenStr).value().claim('tenant').orValue('')`,
			want: "tenant_abc",
		},
		{
			name: "claim_on_optional",
			expr: `jwt.parse(tokenStr).claim('tenant').orValue('')`,
			want: "tenant_abc",
		},
		{
			name: "claim_missing",
			expr: `jwt.parse(tokenStr).claim('nonexistent').hasValue()`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpr(t, env, tc.expr, vars)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %v (%T), want %v (%T)", tc.expr, got, got, tc.want, tc.want)
			}
		})
	}

	// Invalid token formats return CEL evaluation errors
	invalidTokens := []string{"invalid-token", "a.b.c.d", ""}
	for _, inv := range invalidTokens {
		ast, _ := env.Compile(`jwt.parse(tok).hasValue()`)
		prg, _ := env.Program(ast)
		if _, _, err := prg.Eval(map[string]any{"tok": inv}); err == nil {
			t.Errorf("expected error for invalid token format %q, got nil", inv)
		}
	}
}

func TestParseUnverifiedTokenAndFieldVariations(t *testing.T) {
	header := map[string]any{
		"alg": "ES256",
		"kid": "k-42",
	}
	payload := map[string]any{
		"iss": "https://accounts.google.com",
		"sub": "10987654321",
		"aud": "my-client-id",
		"exp": 1700000000,
		"nbf": "1699990000",
		"iat": 1699990000.5,
	}

	tokStr := createTestJWT(t, header, payload)

	tok, err := jwt.ParseToken(tokStr)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"alg", tok.Algorithm, "ES256"},
		{"issuer", tok.Issuer, "https://accounts.google.com"},
		{"subject", tok.Subject, "10987654321"},
		{"key_id", tok.KeyID, "k-42"},
		{"audience", tok.Audience, []string{"my-client-id"}},
		{"exp", tok.ExpiresAt.Unix(), int64(1700000000)},
		{"nbf", tok.NotBefore.Unix(), int64(1699990000)},
		{"iat", tok.IssuedAt.Unix(), int64(1699990000)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestClaimsCustomTypes(t *testing.T) {
	tok := &jwt.Token{
		Payload: map[string]any{
			"intNum":      json.Number("42"),
			"floatNum":    json.Number("3.14"),
			"strNum":      json.Number("NaN"),
			"rawJSON":     json.RawMessage(`{"nested":"value"}`),
			"rawMsgBad":   json.RawMessage(`bad-json`),
			"badNumFloat": json.Number("not-a-number"),
			"simpleStr":   "hello",
		},
	}

	adapter := types.DefaultTypeAdapter

	tests := []struct {
		name      string
		claimName string
		validate  func(t *testing.T, val ref.Val)
	}{
		{
			name:      "json_number_int",
			claimName: "intNum",
			validate: func(t *testing.T, val ref.Val) {
				if val.Value() != int64(42) {
					t.Errorf("expected 42, got %v", val.Value())
				}
			},
		},
		{
			name:      "json_number_float",
			claimName: "floatNum",
			validate: func(t *testing.T, val ref.Val) {
				if val.Value() != float64(3.14) {
					t.Errorf("expected 3.14, got %v", val.Value())
				}
			},
		},
		{
			name:      "json_number_nan_string",
			claimName: "strNum",
			validate: func(t *testing.T, val ref.Val) {
				if val == types.OptionalNone {
					t.Errorf("expected non-empty optional for strNum")
				}
			},
		},
		{
			name:      "raw_json_message",
			claimName: "rawJSON",
			validate: func(t *testing.T, val ref.Val) {
				if val == types.OptionalNone {
					t.Errorf("expected rawJSON to be parsed")
				}
			},
		},
		{
			name:      "raw_msg_bad_conversion_error",
			claimName: "rawMsgBad",
			validate: func(t *testing.T, val ref.Val) {
				if !types.IsError(val) {
					t.Errorf("expected error ref.Val for invalid json.RawMessage, got %v (%T)", val, val)
				}
			},
		},
		{
			name:      "bad_num_conversion_error",
			claimName: "badNumFloat",
			validate: func(t *testing.T, val ref.Val) {
				if !types.IsError(val) {
					t.Errorf("expected error ref.Val for invalid json.Number, got %v (%T)", val, val)
				}
			},
		},
		{
			name:      "simple_string",
			claimName: "simpleStr",
			validate: func(t *testing.T, val ref.Val) {
				if val.Value() != "hello" {
					t.Errorf("expected 'hello', got %v", val.Value())
				}
			},
		},
		{
			name:      "nonexistent_claim",
			claimName: "nonexistent",
			validate: func(t *testing.T, val ref.Val) {
				if val != types.OptionalNone {
					t.Errorf("expected None for nonexistent claim, got %v", val)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val := tok.Claim(adapter, tc.claimName)
			tc.validate(t, val)
		})
	}
}

func TestJWTTimestampTypes(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}

	tests := []struct {
		name    string
		payload map[string]any
		wantExp int64
		wantIat int64
		wantNbf int64
	}{
		{
			name: "float64_and_string",
			payload: map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": float64(1700000000.5),
				"iat": int64(1699990000),
				"nbf": "1699990000",
			},
			wantExp: 1700000000,
			wantIat: 1699990000,
			wantNbf: 1699990000,
		},
		{
			name: "uint64_int32_float32",
			payload: map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": uint64(1700000000),
				"iat": int32(1699990000),
				"nbf": float32(1699990000),
			},
			wantExp: 1700000000,
			wantIat: 1699990000,
			wantNbf: 1699990000,
		},
		{
			name: "int_uint_uint32",
			payload: map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": int(1700000000),
				"iat": uint(1699990000),
				"nbf": uint32(1699990000),
			},
			wantExp: 1700000000,
			wantIat: 1699990000,
			wantNbf: 1699990000,
		},
		{
			name: "string_float",
			payload: map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": "1700000000.5",
				"iat": 1699900000,
			},
			wantExp: 1700000000,
			wantIat: 1699900000,
			wantNbf: 0,
		},
		{
			name: "json_number_float",
			payload: map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": json.Number("1700000000.75"),
				"iat": 1699900000,
			},
			wantExp: 1700000000,
			wantIat: 1699900000,
			wantNbf: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tokStr := createTestJWT(t, header, tc.payload)
			tok, err := jwt.ParseToken(tokStr)
			if err != nil {
				t.Fatalf("ParseToken failed: %v", err)
			}
			if tok.ExpiresAt.Unix() != tc.wantExp {
				t.Errorf("exp = %v, want %v", tok.ExpiresAt.Unix(), tc.wantExp)
			}
			if tok.IssuedAt.Unix() != tc.wantIat {
				t.Errorf("iat = %v, want %v", tok.IssuedAt.Unix(), tc.wantIat)
			}
			if tc.wantNbf != 0 && tok.NotBefore.Unix() != tc.wantNbf {
				t.Errorf("nbf = %v, want %v", tok.NotBefore.Unix(), tc.wantNbf)
			}
		})
	}
}

func TestJWTValidateTimesOption(t *testing.T) {
	fixedNow := time.Unix(1700000000, 0).UTC()
	header := map[string]any{"alg": "RS256", "typ": "JWT"}

	tokValid := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-aud",
		"iat": fixedNow.Add(-1 * time.Hour).Unix(),
		"nbf": fixedNow.Add(-1 * time.Hour).Unix(),
		"exp": fixedNow.Add(1 * time.Hour).Unix(),
	})

	tokExpired := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-aud",
		"iat": fixedNow.Add(-2 * time.Hour).Unix(),
		"exp": fixedNow.Add(-10 * time.Minute).Unix(),
	})

	tokFutureNbf := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-aud",
		"iat": fixedNow.Add(-1 * time.Hour).Unix(),
		"nbf": fixedNow.Add(10 * time.Minute).Unix(),
		"exp": fixedNow.Add(1 * time.Hour).Unix(),
	})

	tokFutureIat := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-aud",
		"iat": fixedNow.Add(10 * time.Minute).Unix(),
		"exp": fixedNow.Add(1 * time.Hour).Unix(),
	})

	tokInvertedWindow := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-aud",
		"iat": fixedNow.Add(-30 * time.Minute).Unix(),
		"nbf": fixedNow.Add(-10 * time.Minute).Unix(),
		"exp": fixedNow.Add(-20 * time.Minute).Unix(),
	})

	tests := []struct {
		name     string
		options  []jwt.Option
		tokenStr string
		wantPass bool
	}{
		{
			name:     "default_no_validation_allows_expired",
			options:  nil,
			tokenStr: tokExpired,
			wantPass: true,
		},
		{
			name:     "validated_valid_token_passes",
			options:  []jwt.Option{jwt.ValidateTimes(), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokValid,
			wantPass: true,
		},
		{
			name:     "validated_expired_token_rejected",
			options:  []jwt.Option{jwt.ValidateTimes(), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokExpired,
			wantPass: false,
		},
		{
			name:     "validated_future_nbf_rejected",
			options:  []jwt.Option{jwt.ValidateTimes(), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokFutureNbf,
			wantPass: false,
		},
		{
			name:     "validated_future_iat_rejected",
			options:  []jwt.Option{jwt.ValidateTimes(), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokFutureIat,
			wantPass: false,
		},
		{
			name:     "leeway_allows_token_expired_within_window",
			options:  []jwt.Option{jwt.ValidateTimes(30 * time.Minute), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokExpired,
			wantPass: true,
		},
		{
			name:     "leeway_rejects_inverted_nbf_after_exp",
			options:  []jwt.Option{jwt.ValidateTimes(30 * time.Minute), jwt.Clock(func() time.Time { return fixedNow })},
			tokenStr: tokInvertedWindow,
			wantPass: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, err := cel.NewEnv(
				jwt.Library(tc.options...),
				cel.Variable("tok", cel.StringType),
			)
			if err != nil {
				t.Fatalf("cel.NewEnv failed: %v", err)
			}
			got := evalExpr(t, env, `jwt.parse(tok).hasValue()`, map[string]any{"tok": tc.tokenStr})
			if got != tc.wantPass {
				t.Errorf("jwt.parse(tok).hasValue() = %v, want %v", got, tc.wantPass)
			}
		})
	}
}

func TestJWTOptionalReceiverChaining(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	goodTokStr := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-client",
		"tag": "prod",
		"exp": 1700000000,
		"iat": 1699900000,
	})

	env, err := cel.NewEnv(
		jwt.Library(
			jwt.Version(1),
			jwt.ClockLeeway(10*time.Minute),
		),
		cel.Variable("tok", cel.StringType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}

	envExpired, err := cel.NewEnv(
		jwt.Library(jwt.ValidateTimes(), jwt.Clock(func() time.Time { return time.Unix(2000000000, 0) })),
		cel.Variable("tok", cel.StringType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}

	tests := []struct {
		name string
		env  *cel.Env
		expr string
		want any
	}{
		{
			name: "presented_by_match",
			env:  env,
			expr: `jwt.parse(tok).presentedBy('https://auth.example.com', 'my-client')`,
			want: true,
		},
		{
			name: "presented_by_mismatch",
			env:  env,
			expr: `jwt.parse(tok).presentedBy('https://auth.example.com', 'wrong-client')`,
			want: false,
		},
		{
			name: "presented_by_on_optional_none",
			env:  envExpired,
			expr: `jwt.parse(tok).presentedBy('https://auth.example.com', 'my-client')`,
			want: false,
		},
		{
			name: "claim_present",
			env:  env,
			expr: `jwt.parse(tok).claim('tag').orValue('default')`,
			want: "prod",
		},
		{
			name: "claim_on_optional_none",
			env:  envExpired,
			expr: `jwt.parse(tok).claim('tag').orValue('default')`,
			want: "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpr(t, tc.env, tc.expr, map[string]any{"tok": goodTokStr})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestJWTDirectTokenVariables(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	goodTokStr := createTestJWT(t, header, map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-client",
		"tag": "prod",
		"exp": 1700000000,
		"iat": 1699900000,
	})

	tok, err := jwt.ParseToken(goodTokStr)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}

	env, err := cel.NewEnv(
		jwt.Library(),
		cel.Variable("t", cel.ObjectType("jwt.Token")),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}

	vars := map[string]any{"t": tok}

	tests := []struct {
		name string
		expr string
		want any
	}{
		{
			name: "presented_by_direct_match",
			expr: `t.presentedBy('https://auth.example.com', 'my-client')`,
			want: true,
		},
		{
			name: "presented_by_direct_mismatch",
			expr: `t.presentedBy('https://auth.example.com', 'wrong-client')`,
			want: false,
		},
		{
			name: "claim_direct_present",
			expr: `t.claim('tag').orValue('')`,
			want: "prod",
		},
		{
			name: "claim_direct_missing",
			expr: `t.claim('nonexistent').orValue('default')`,
			want: "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpr(t, env, tc.expr, vars)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestJWTPresentedByWithAuthorizedPartyAZP(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}

	tokPayload := map[string]any{
		"iss": "https://accounts.google.com",
		"sub": "user-456",
		"aud": "https://api.example.com",
		"azp": "frontend-client-app-id",
		"exp": 1700000000,
		"iat": 1699900000,
	}
	tokStr := createTestJWT(t, header, tokPayload)

	tokNoAZP := createTestJWT(t, header, map[string]any{
		"iss": "https://accounts.google.com",
		"sub": "user-456",
		"aud": "https://api.example.com",
		"exp": 1700000000,
		"iat": 1699900000,
	})

	env, err := cel.NewEnv(
		jwt.Library(),
		cel.Variable("tokStr", cel.StringType),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}

	tests := []struct {
		name     string
		expr     string
		tokenStr string
		want     any
	}{
		{
			name:     "azp_field_access",
			expr:     `jwt.parse(tokStr).value().azp`,
			tokenStr: tokStr,
			want:     "frontend-client-app-id",
		},
		{
			name:     "presented_by_matches_azp",
			expr:     `jwt.parse(tokStr).presentedBy('https://accounts.google.com', 'frontend-client-app-id')`,
			tokenStr: tokStr,
			want:     true,
		},
		{
			name:     "presented_by_rejects_aud_when_azp_exists",
			expr:     `jwt.parse(tokStr).presentedBy('https://accounts.google.com', 'https://api.example.com')`,
			tokenStr: tokStr,
			want:     false,
		},
		{
			name:     "presented_by_falls_back_to_aud_when_azp_omitted",
			expr:     `jwt.parse(tokStr).presentedBy('https://accounts.google.com', 'https://api.example.com')`,
			tokenStr: tokNoAZP,
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evalExpr(t, env, tc.expr, map[string]any{"tokStr": tc.tokenStr})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestJWTParsingErrorsAndEncodings(t *testing.T) {
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	validPayload := map[string]any{
		"iss": "https://auth.example.com",
		"sub": "user-123",
		"aud": "my-client",
		"exp": 1700000000,
		"iat": 1699900000,
	}

	goodHeaderB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	validPayloadBytes, _ := json.Marshal(validPayload)
	validPayloadB64 := base64.RawURLEncoding.EncodeToString(validPayloadBytes)
	badJSONB64 := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))

	tests := []struct {
		name     string
		tokenStr string
		errMsg   string
	}{
		{
			name:     "one_segment",
			tokenStr: "one",
			errMsg:   "invalid token format",
		},
		{
			name:     "four_segments",
			tokenStr: "one.two.three.four",
			errMsg:   "invalid token format",
		},
		{
			name:     "empty_token",
			tokenStr: "",
			errMsg:   "invalid token format",
		},
		{
			name:     "bad_header_base64",
			tokenStr: "!bad!.payload.sig",
			errMsg:   "failed to decode header",
		},
		{
			name:     "non_json_header",
			tokenStr: badJSONB64 + "." + validPayloadB64 + ".sig",
			errMsg:   "failed to parse header JSON",
		},
		{
			name:     "missing_header_alg",
			tokenStr: base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT"}`)) + "." + validPayloadB64 + ".sig",
			errMsg:   "missing required header: 'alg'",
		},
		{
			name:     "bad_payload_base64",
			tokenStr: goodHeaderB64 + ".!bad!.sig",
			errMsg:   "failed to decode payload",
		},
		{
			name:     "non_json_payload",
			tokenStr: goodHeaderB64 + "." + badJSONB64 + ".sig",
			errMsg:   "failed to parse payload JSON",
		},
		{
			name:     "exceeds_max_token_size",
			tokenStr: strings.Repeat("a", 11*1024*1024),
			errMsg:   "token size exceeds maximum allowed limit",
		},
		{
			name: "malformed_nbf_claim",
			tokenStr: createTestJWT(t, header, map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": "my-client",
				"exp": 1700000000,
				"iat": 1699900000,
				"nbf": "invalid-timestamp",
			}),
			errMsg: "invalid claim 'nbf'",
		},
		{
			name: "non_string_element_in_aud_list",
			tokenStr: createTestJWT(t, header, map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": []any{"client-1", 12345},
				"exp": 1700000000,
				"iat": 1699900000,
			}),
			errMsg: "invalid claim 'aud'",
		},
		{
			name: "invalid_aud_type",
			tokenStr: createTestJWT(t, header, map[string]any{
				"iss": "https://auth.example.com",
				"sub": "user-123",
				"aud": 12345,
				"exp": 1700000000,
				"iat": 1699900000,
			}),
			errMsg: "invalid claim 'aud'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := jwt.ParseToken(tc.tokenStr)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errMsg)
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("error = %q, want error containing %q", err.Error(), tc.errMsg)
			}
		})
	}

	requiredClaims := []string{"iss", "sub", "aud", "exp", "iat"}
	for _, claim := range requiredClaims {
		t.Run("missing_claim_"+claim, func(t *testing.T) {
			p := make(map[string]any)
			for k, v := range validPayload {
				if k != claim {
					p[k] = v
				}
			}
			tokStr := createTestJWT(t, header, p)
			if _, err := jwt.ParseToken(tokStr); err == nil {
				t.Errorf("expected error when missing required claim %q, got nil", claim)
			}
		})
	}
}

func TestTokenIsValidAt(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	leeway := 5 * time.Minute

	tests := []struct {
		name      string
		token     jwt.Token
		refTime   time.Time
		leeway    time.Duration
		wantValid bool
	}{
		{
			name: "valid token within active window",
			token: jwt.Token{
				IssuedAt:  now.Add(-1 * time.Hour),
				NotBefore: now.Add(-30 * time.Minute),
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    0,
			wantValid: true,
		},
		{
			name: "issued-at exactly now",
			token: jwt.Token{
				IssuedAt:  now,
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    0,
			wantValid: true,
		},
		{
			name: "issued-at in future within leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(3 * time.Minute),
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: true,
		},
		{
			name: "issued-at in future beyond leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(10 * time.Minute),
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: false,
		},
		{
			name: "not-before in future within leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(-10 * time.Minute),
				NotBefore: now.Add(3 * time.Minute),
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: true,
		},
		{
			name: "not-before in future beyond leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(-10 * time.Minute),
				NotBefore: now.Add(10 * time.Minute),
				ExpiresAt: now.Add(1 * time.Hour),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: false,
		},
		{
			name: "expired token in past within leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(-1 * time.Hour),
				ExpiresAt: now.Add(-3 * time.Minute),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: true,
		},
		{
			name: "expired token in past beyond leeway",
			token: jwt.Token{
				IssuedAt:  now.Add(-1 * time.Hour),
				ExpiresAt: now.Add(-10 * time.Minute),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: false,
		},
		{
			name: "expired token exactly at negative leeway boundary",
			token: jwt.Token{
				IssuedAt:  now.Add(-1 * time.Hour),
				ExpiresAt: now.Add(-5 * time.Minute),
			},
			refTime:   now,
			leeway:    leeway,
			wantValid: false,
		},
		{
			name: "inverted nbf > exp",
			token: jwt.Token{
				IssuedAt:  now.Add(-1 * time.Hour),
				NotBefore: now.Add(30 * time.Minute),
				ExpiresAt: now.Add(15 * time.Minute),
			},
			refTime:   now,
			leeway:    0,
			wantValid: false,
		},
		{
			name: "inverted iat > exp",
			token: jwt.Token{
				IssuedAt:  now.Add(30 * time.Minute),
				ExpiresAt: now.Add(15 * time.Minute),
			},
			refTime:   now,
			leeway:    1 * time.Hour,
			wantValid: false,
		},
		{
			name: "empty claims (all zero time)",
			token: jwt.Token{
				Issuer: "https://auth.example.com",
			},
			refTime:   now,
			leeway:    0,
			wantValid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.token.IsValidAt(tc.refTime, tc.leeway)
			if got != tc.wantValid {
				t.Errorf("token.IsValidAt(%v, %v) = %v, want %v", tc.refTime, tc.leeway, got, tc.wantValid)
			}
		})
	}
}
