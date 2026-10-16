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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

type internalContextVariable struct {
	Type     string `yaml:"type,omitempty" json:"type,omitempty"`
	TypeName string `yaml:"type_name,omitempty" json:"typeName,omitempty"`
}

type internalOverload Overload

type internalTypeDesc TypeDesc

// Embedding TypeDesc in variable causes issues with customizing
// unmarshalling / marshalling. Work around with a parallel type.
type internalVariable struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Type        *TypeDesc   `yaml:"type,omitempty" json:"type,omitempty"`
	TypeName    string      `yaml:"type_name" json:"typeName,omitempty"`
	Params      []*TypeDesc `yaml:"params,omitempty" json:"params,omitempty"`
	IsTypeParam bool        `yaml:"is_type_param,omitempty" json:"isTypeParam,omitempty"`
}

type internalValidator Validator

func jsonUnmarshaler(data []byte) func(any) error {
	return func(v any) error {
		return json.Unmarshal(data, v)
	}
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (ctx *ContextVariable) UnmarshalYAML(unmarshal func(any) error) error {
	var buf internalContextVariable
	if err := unmarshal(&buf); err != nil {
		return err
	}
	if buf.TypeName != "" {
		ctx.TypeName = buf.TypeName
	} else if buf.Type != "" {
		ctx.TypeName = buf.Type
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (ctx *ContextVariable) UnmarshalJSON(data []byte) error {
	return ctx.UnmarshalYAML(jsonUnmarshaler(data))
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (od *Overload) UnmarshalYAML(unmarshal func(any) error) error {
	var id string
	if err := unmarshal(&id); err == nil {
		od.ID = id
		return nil
	}
	var buf internalOverload
	if err := unmarshal(&buf); err != nil {
		return fmt.Errorf("unsupported overload encoding: %w", err)
	}
	*od = Overload(buf)
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (od *Overload) UnmarshalJSON(data []byte) error {
	return od.UnmarshalYAML(jsonUnmarshaler(data))
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (v *Variable) UnmarshalYAML(unmarshal func(any) error) error {
	var buf internalVariable
	if err := unmarshal(&buf); err != nil {
		return err
	}
	v.fromInternal(&buf)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (v *Variable) MarshalYAML() (any, error) {
	// The presence of an unmarshaller alters the default marshaller behavior so
	// provide a simple marshal implementation.
	return v.toInternal(), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (v *Variable) UnmarshalJSON(data []byte) error {
	return v.UnmarshalYAML(jsonUnmarshaler(data))
}

// MarshalJSON implements json.Marshaler.
func (v *Variable) MarshalJSON() ([]byte, error) {
	return marshalJSONNoEscape(&internalVariable{
		Name:        v.Name,
		Description: v.Description,
		Type:        v.GetType(),
	}, "")
}

func (v *Variable) fromInternal(buf *internalVariable) {
	v.Name = buf.Name
	v.Description = buf.Description
	if buf.TypeName != "" {
		v.TypeDesc = &TypeDesc{
			TypeName:    buf.TypeName,
			Params:      buf.Params,
			IsTypeParam: buf.IsTypeParam,
		}
	} else if buf.Type != nil {
		v.TypeDesc = buf.Type
	}
}

func (v *Variable) toInternal() *internalVariable {
	buf := &internalVariable{
		Name:        v.Name,
		Description: v.Description,
	}
	if t := v.GetType(); t != nil {
		buf.TypeName = t.TypeName
		buf.Params = t.Params
		buf.IsTypeParam = t.IsTypeParam
	}
	return buf
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (td *TypeDesc) UnmarshalYAML(unmarshal func(any) error) error {
	var str string
	if err := unmarshal(&str); err == nil {
		o, err := ParseTypeDesc(str)
		if err != nil {
			return err
		}
		*td = *o
		return nil
	}
	var buf internalTypeDesc
	if err := unmarshal(&buf); err != nil {
		return fmt.Errorf("unsupported type description encoding: %w", err)
	}
	*td = TypeDesc(buf)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (td *TypeDesc) MarshalJSON() ([]byte, error) {
	if td == nil {
		return []byte("null"), nil
	}
	return marshalJSONNoEscape(td.SpecifierFormat(), "")
}

// UnmarshalJSON implements json.Unmarshaler.
func (td *TypeDesc) UnmarshalJSON(data []byte) error {
	return td.UnmarshalYAML(jsonUnmarshaler(data))
}

// UnmarshalJSON implements json.Unmarshaler.
func (v *Validator) UnmarshalJSON(data []byte) error {
	var buf internalValidator
	if err := json.Unmarshal(data, &buf); err != nil {
		return err
	}
	for k, val := range buf.Config {
		if num, ok := val.(float64); ok && num == float64(int(num)) {
			buf.Config[k] = int(num)
		}
	}
	*v = Validator(buf)
	return nil
}

type typeDescParser struct {
	text   string
	pos    int
	length int
}

// ParseTypeDesc parses a TypeDesc from the type specifier format: "map<string, int>"
func ParseTypeDesc(text string) (*TypeDesc, error) {
	p := &typeDescParser{text: text, length: len(text)}
	res, err := p.parseTypeElem()
	if err != nil {
		return nil, fmt.Errorf("failed to parse type %q: %v", text, err)
	}
	p.skipWhitespace()
	if p.pos < p.length {
		return nil, fmt.Errorf("unexpected character %q at position %d in %q", p.text[p.pos], p.pos, text)
	}
	return res, nil
}

func (p *typeDescParser) parseConcreteType() (*TypeDesc, error) {
	id, err := p.parseNamespaceIdentifier()
	if err != nil {
		return nil, err
	}
	if p.pos < p.length && p.text[p.pos] == '<' {
		p.pos++ // consume '<'
		var params []*TypeDesc
		for {
			p.skipWhitespace()
			param, err := p.parseTypeElem()
			if err != nil {
				return nil, err
			}
			params = append(params, param)
			p.skipWhitespace()
			if p.pos < p.length && p.text[p.pos] == ',' {
				p.pos++ // consume ','
				continue
			}
			if p.pos < p.length && p.text[p.pos] == '>' {
				p.pos++ // consume '>'
				break
			}
			return nil, fmt.Errorf("expected ',' or '>' at position %d", p.pos)
		}
		return NewTypeDesc(id, params...), nil
	}
	return NewTypeDesc(id), nil
}

func (p *typeDescParser) parseTypeElem() (*TypeDesc, error) {
	p.skipWhitespace()
	if p.pos < p.length && p.text[p.pos] == '~' {
		p.pos++ // consume '~'
		id, err := p.parseTypeParamIdent()
		if err != nil {
			return nil, err
		}
		return NewTypeParam(id), nil
	}
	return p.parseConcreteType()
}

func (p *typeDescParser) parseNamespaceIdentifier() (string, error) {
	p.skipWhitespace()
	var id string
	for p.pos < p.length && p.text[p.pos] != '<' {
		c := p.text[p.pos]
		if c == '.' {
			id += "."
			p.pos++ // consume '.'
		}
		ident, err := p.parseIdentifier()
		if err != nil {
			return "", err
		}
		id += ident
		p.skipWhitespace()
		if p.pos < p.length && p.text[p.pos] != '.' {
			break
		}
	}
	if id == "" {
		return "", fmt.Errorf("missing identifier at position %d", p.pos)
	}
	return id, nil
}

func (p *typeDescParser) parseIdentifier() (string, error) {
	p.skipWhitespace()
	if p.pos >= p.length {
		return "", fmt.Errorf("unexpected end of input")
	}
	start := p.pos
	c := p.text[p.pos]
	if !isAlpha(c) && c != '_' {
		return "", fmt.Errorf("identifier is expected, but %q was found at position %d", c, p.pos)
	}
	p.pos++
	for p.pos < p.length {
		c := p.text[p.pos]
		if !isAlphaNumeric(c) && c != '_' {
			break
		}
		p.pos++
	}
	return p.text[start:p.pos], nil
}

func (p *typeDescParser) parseTypeParamIdent() (string, error) {
	p.skipWhitespace()
	if p.pos >= p.length {
		return "", fmt.Errorf("unexpected end of input")
	}
	c := p.text[p.pos]
	if !isAlpha(c) {
		return "", fmt.Errorf("invalid type parameter identifier %q at position %d, must be a single character from A-Z", c, p.pos)
	}
	p.pos++
	if p.pos < p.length && isAlpha(p.text[p.pos]) {
		return "", fmt.Errorf("invalid type param, must have a single alphabetic character at position %d", p.pos)
	}
	return string(c), nil
}

func (p *typeDescParser) skipWhitespace() {
	for p.pos < p.length && isWhitespace(p.text[p.pos]) {
		p.pos++
	}
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphaNumeric(c byte) bool {
	return isAlpha(c) || (c >= '0' && c <= '9')
}

// DecodeOption configures how an environment Config is decoded from YAML or JSON.
type DecodeOption func(*decodeOptions)

type decodeOptions struct {
	strict bool
}

// StrictDecoding configures whether unknown fields in the YAML or JSON input cause a decoding error.
// By default, unknown fields are ignored.
func StrictDecoding(strict bool) DecodeOption {
	return func(opts *decodeOptions) {
		opts.strict = strict
	}
}

func validateStrictJSON(v any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch val := v.(type) {
	case []any:
		if t.Kind() == reflect.Slice {
			for _, elem := range val {
				if err := validateStrictJSON(elem, t.Elem()); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		switch t {
		case reflect.TypeFor[ContextVariable]():
			t = reflect.TypeFor[internalContextVariable]()
		case reflect.TypeFor[Variable]():
			t = reflect.TypeFor[internalVariable]()
		}
		if t.Kind() != reflect.Struct {
			return nil
		}
		for k, child := range val {
			ft, ok := jsonFieldType(t, k)
			if !ok {
				return fmt.Errorf("json: unknown field %q", k)
			}
			if err := validateStrictJSON(child, ft); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonFieldType(t reflect.Type, name string) (reflect.Type, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == name {
			return f.Type, true
		}
	}
	return nil, false
}

// ConfigFromYAML returns a config from YAML source.
//
// Adds custom parsing logic for normalizing shorthand for specifiying some fields
// in a YAML document (mainly the type-specifier shorthand).
//
// Using yaml.Unmarshal with any implementation should be sufficient for most
// cases.
func ConfigFromYAML(data []byte, opts ...DecodeOption) (*Config, error) {
	dOpts := &decodeOptions{}
	for _, opt := range opts {
		opt(dOpts)
	}
	c := &Config{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(dOpts.strict)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return c, nil
}

// ConfigToYAML returns the config serialized to YAML
//
// Provided as a convenience wrapper around a tested YAML Marshaler.
func ConfigToYAML(c *Config) ([]byte, error) {
	return yaml.Marshal(c)
}

// ConfigFromJSON returns a config from JSON source.
//
// Adds custom parsing logic for normalizing shorthand for specifying some fields
// in a JSON document (mainly the type-specifier shorthand).
//
// Using json.Unmarshal with any implementation should be sufficient for most
// cases.
func ConfigFromJSON(data []byte, opts ...DecodeOption) (*Config, error) {
	dOpts := &decodeOptions{}
	for _, opt := range opts {
		opt(dOpts)
	}
	c := &Config{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	if dOpts.strict {
		var raw any
		_ = json.Unmarshal(data, &raw)
		if err := validateStrictJSON(raw, reflect.TypeFor[Config]()); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// ConfigToJSON returns the config serialized to JSON
//
// Provided as a convenience wrapper around a tested JSON Marshaler.
func ConfigToJSON(c *Config) ([]byte, error) {
	return marshalJSONNoEscape(c, "  ")
}

func marshalJSONNoEscape(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
