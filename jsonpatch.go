// Copyright (c) 2024 hanke. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.

// Package jsonpatch provides a json patch library.
package jsonpatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

const (
	opAdd     = "add"
	opRemove  = "remove"
	opReplace = "replace"
	opMove    = "move"
	opCopy    = "copy"
	opTest    = "test"
)

var (
	// ErrStop is a stop error. Any extension can return this error to stop the patch.
	// For example, the "test" extension can return this error to stop the patch.
	ErrStop = errors.New("stop")
	// ErrNotExists is a not exists error.
	// If StrictPathExists is false, the patch will continue if extension return this error.
	ErrNotExists = errors.New("path member not exists")
)

// Extension is a jsonpatch extension that apply an operation.
type Extension interface {
	// OP returns the operation name
	// e.g. "add", "remove", "replace", "move", "copy", "test"
	OP() string
	// Apply apply the operation
	// op is check before apply.
	Apply(p *Patch, o *any, op Operation) error
	// Check check the operation, and return error if the operation is invalid.
	Check(p *Patch, op Operation) error
}

// Descriptor is a jsonpatch extension that return a description of the operation
type Descriptor interface {
	// Description return a description of the operation
	Description(p *Patch, op Operation) string
}

// Operation is a jsonpatch operation introduced in RFC6902.
type Operation struct {
	OP   *string `json:"op"`
	Path *string `json:"path"`
	// Value is the value of the operation.
	// Unlike json Unmarshal, if the value is null, it will not be set to nil, but a pointer to nil.
	Value *any    `json:"value,omitempty"`
	From  *string `json:"from,omitempty"`
}

func (o Operation) check() error {
	if o.OP == nil {
		return errors.New("must contains an op member")
	}
	if o.Path == nil {
		return errors.New("must contains a path member")
	}
	if err := NewJSONPointer(*o.Path).Check(); err != nil {
		return err
	}
	if o.From != nil {
		if err := NewJSONPointer(*o.From).Check(); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *Operation) UnmarshalJSON(data []byte) error {
	m := map[string]any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	setValueFromMap(&o.OP, m, "op")
	setValueFromMap(&o.Path, m, "path")
	setAnyFromMap(&o.Value, m, "value")
	setValueFromMap(&o.From, m, "from")
	return nil
}

func setValueFromMap[T any](dst **T, src map[string]any, key string) {
	v, ok := src[key]
	if !ok {
		return
	}
	t, ok := v.(T)
	if !ok {
		return
	}
	*dst = &t
}

func setAnyFromMap(dst **any, src map[string]any, key string) {
	v, ok := src[key]
	if !ok {
		return
	}
	*dst = &v
}

// JSONPointer is a json pointer introduce in RFC 6901.
type JSONPointer struct {
	origin string
}

// NewJSONPointer create a new JSONPointer.
func NewJSONPointer(p string) JSONPointer {
	return JSONPointer{origin: p}
}

// IsTheWholeDocument return true if the JSONPointer points to the whole document.
func (p JSONPointer) IsTheWholeDocument() bool {
	return p.origin == ""
}

// Path return the every parts of the JSONPointer.
func (p JSONPointer) Path() []string {
	v := strings.Split(p.origin, "/")
	for i, d := range v {
		v[i] = unescapePath(d)
	}
	return v[1:]
}

// Check check the JSONPointer, and return error if the JSONPointer is invalid.
func (p JSONPointer) Check() error {
	if p.origin == "" {
		return nil
	}
	if p.origin[0] != '/' {
		return errors.New("json pointer must start with /")
	}
	return nil
}

// ParentPath return the parent path of the JSONPointer.
func (p JSONPointer) ParentPath() []string {
	v := p.Path()
	if len(v) == 0 {
		return nil
	}
	return v[:len(v)-1]
}

// SameParent return true if the parent path of the JSONPointer is the same as the parent path of the other JSONPointer.
func (p JSONPointer) SameParent(o JSONPointer) bool {
	a := p.ParentPath()
	b := o.ParentPath()
	if a == nil && b == nil {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// LastToken return the last token of the JSONPointer.
func (p JSONPointer) LastToken() string {
	v := p.Path()
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// Patch is a jsonpatch introduced in RFC6902.
type Patch struct {
	// StrictPathExists is a flag that indicates whether to throw an error if the path does not exist.
	StrictPathExists bool
	// SupportNegativeArrayIndex is a flag that indicates whether to support negative array index.
	SupportNegativeArrayIndex bool

	// Standard json marshaling options.
	JSONPrefix     string
	JSONIndent     string
	JSONEscapeHTML bool

	extensions map[string]Extension
}

// Option is a jsonpatch option.
type Option func(o *Patch)

// WithJSONIndent set the JSONIndent option.
func WithJSONIndent(prefix, indent string) Option {
	return func(o *Patch) {
		o.JSONPrefix = prefix
		o.JSONIndent = indent
	}
}

// WithStrictPathExists set the StrictPathExists option.
// The default value is false.
// If StrictPathExists is true, an error will be thrown if the path does not exist.
func WithStrictPathExists(on bool) Option {
	return func(o *Patch) {
		o.StrictPathExists = on
	}
}

// WithJSONEscapeHTML set the JSONEscapeHTML option.
func WithJSONEscapeHTML(on bool) Option {
	return func(o *Patch) {
		o.JSONEscapeHTML = on
	}
}

// WithSupportNegativeArrayIndex set the SupportNegativeArrayIndex option.
// The default value is false.
// If SupportNegativeArrayIndex is true, negative array index is supported.
func WithSupportNegativeArrayIndex(on bool) Option {
	return func(o *Patch) {
		o.SupportNegativeArrayIndex = on
	}
}

// WithExtension  add a new extension.
func WithExtension(ext Extension) Option {
	return func(o *Patch) {
		o.extensions[ext.OP()] = ext
	}
}

// New create a new jsonpatch.
// It exactly matches the RFC6902 spec if no option is set.
func New(options ...Option) *Patch {
	p := &Patch{
		StrictPathExists: true,
		extensions: map[string]Extension{
			opAdd:     addExtension{},
			opRemove:  removeExtension{},
			opReplace: replaceExtension{},
			opMove:    moveExtension{},
			opCopy:    copyExtension{},
			opTest:    testExtension{},
		},
	}
	for _, option := range options {
		option(p)
	}
	return p
}

var ()

var indexRE = regexp.MustCompile(`^(0|([1-9][0-9]*))$`)
var negativeIndexRE = regexp.MustCompile(`^-?(0|([1-9][0-9]*))$`)

// ParseArrayIndex parse the array index.
func (p *Patch) ParseArrayIndex(size int, s string) (i int, err error) {
	if s == "-" {
		if size == 0 {
			return 0, nil
		}
		return size, nil
	}
	if p.SupportNegativeArrayIndex {
		if !negativeIndexRE.MatchString(s) {
			return 0, fmt.Errorf("bad array index: %s", s)
		}
		i, err = strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		if i < 0 {
			i = size + i
		}
	} else {
		if !indexRE.MatchString(s) {
			return 0, fmt.Errorf("bad array index: %s", s)
		}
		i, err = strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		if i < 0 {
			return
		}
	}
	if i < 0 || i > size {
		return 0, fmt.Errorf("array index out of range: size=%d, %s", size, s)
	}
	return i, nil
}

// Setter is a function that sets the value of a node.
type Setter func(n any)

// VisitPath visit the path list.
func (p *Patch) VisitPath(o *any, parts ...string) (any, Setter, error) {
	var (
		node = *o
		set  Setter
		err  error
	)
	for _, part := range parts {
		node, set, err = p.visitPathPart(node, part)
		if err != nil {
			return nil, nil, err
		}
	}
	if set == nil {
		return node, func(n any) { *o = n }, nil
	}
	return node, set, nil
}

func (p *Patch) visitPathPart(o any, part string) (any, Setter, error) {
	switch v := o.(type) {
	case map[string]any:
		g, ok := v[part]
		if !ok {
			return nil, nil, ErrNotExists
		}
		return g, func(n any) { v[part] = n }, nil
	case []any:
		if len(v) == 0 {
			return nil, nil, ErrNotExists
		}
		i, err := p.ParseArrayIndex(len(v), part)
		if err != nil {
			return nil, nil, err
		}
		if i == len(v) {
			return nil, nil, ErrNotExists
		}
		return v[i], func(n any) { v[i] = n }, nil
	default:
		return nil, nil, fmt.Errorf("cannot visit type: %T", o)
	}
}

// AddValue add a value to a node.
func (p *Patch) AddValue(o any, set Setter, key string, value any) (err error) {
	switch v := o.(type) {
	case map[string]any:
		v[key] = value
		return nil
	case []any:
		i, err := p.ParseArrayIndex(len(v), key)
		if err != nil {
			return err
		}
		v = sliceInsert(v, i, value)
		set(v)
		return nil
	default:
		return fmt.Errorf("bad type for add: %T", o)
	}
}

// ReplaceValue replace a value to a node.
func (p *Patch) ReplaceValue(o any, _ Setter, key string, value any) (err error) {
	switch v := o.(type) {
	case map[string]any:
		if p.StrictPathExists {
			if _, ok := v[key]; !ok {
				return ErrNotExists
			}
		}
		v[key] = value
		return nil
	case []any:
		i, err := p.ParseArrayIndex(len(v), key)
		if err != nil {
			return err
		}
		if len(v) == i {
			if p.StrictPathExists {
				return ErrNotExists
			}
			return nil
		}
		v[i] = value
		return nil
	default:
		return fmt.Errorf("bad type for replace: %T", o)
	}
}

// RemoveValue remove a value from a node.
func (p *Patch) RemoveValue(o any, set Setter, key string) (err error) {
	switch v := o.(type) {
	case map[string]any:
		if p.StrictPathExists {
			_, ok := v[key]
			if !ok {
				return ErrNotExists
			}
		}
		delete(v, key)
		return nil
	case []any:
		i, err := p.ParseArrayIndex(len(v), key)
		if err != nil {
			if p.StrictPathExists {
				return ErrNotExists
			}
			return nil
		}
		if len(v) == i {
			if p.StrictPathExists {
				return ErrNotExists
			}
			return nil
		} else {
			v = sliceRemove(v, i)
		}
		set(v)
		return nil
	default:
		return fmt.Errorf("bad type for remove: %T", o)
	}
}

// MoveValue move a value from a node.
func (p *Patch) MoveValue(o any, set Setter, from, to string) (err error) {
	switch v := o.(type) {
	case map[string]any:
		if p.StrictPathExists {
			_, ok := v[from]
			if !ok {
				return ErrNotExists
			}
		}
		e, ok := v[from]
		if !ok {
			return
		}
		delete(v, from)
		v[to] = e
		return nil
	case []any:
		fi, err := p.ParseArrayIndex(len(v), from)
		if err != nil {
			if p.StrictPathExists {
				return ErrNotExists
			}
			return nil
		}
		ti, err := p.ParseArrayIndex(len(v), to)
		if err != nil {
			if p.StrictPathExists {
				return ErrNotExists
			}
			return nil
		}
		if fi == ti {
			return nil
		}
		e := v[fi]
		v = sliceRemove(v, fi)
		v = sliceInsert(v, ti, e)
		v[ti] = e
		set(v)
		return nil
	default:
		return fmt.Errorf("bad type for move: %T", o)
	}
}

// Check check the operations.
func (p *Patch) Check(ops []Operation) error {
	for _, op := range ops {
		if err := op.check(); err != nil {
			return err
		}
		e := p.extensions[*op.OP]
		if e == nil {
			return fmt.Errorf("unknown operation: %s", *op.OP)
		}
		if err := e.Check(p, op); err != nil {
			return fmt.Errorf("%w: %+v", err, op)
		}
	}
	return nil
}

// Apply apply the operations.
func (p *Patch) Apply(b []byte, ops []Operation) ([]byte, error) {
	var o any
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	if err := p.applyAny(&o, ops); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(p.JSONEscapeHTML)
	enc.SetIndent(p.JSONPrefix, p.JSONIndent)
	if err := enc.Encode(o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ApplyAny apply the operations.
func (p *Patch) ApplyAny(o *any, ops []Operation) error {
	if o == nil {
		return fmt.Errorf("bad type for apply: %T", o)
	}
	switch (*o).(type) {
	case map[string]interface{}:
	case []interface{}:
	default:
		return fmt.Errorf("bad type for apply: %T", o)
	}
	return p.applyAny(o, ops)
}

func (p *Patch) applyAny(o *any, ops []Operation) error {
	if err := p.Check(ops); err != nil {
		return err
	}
	for _, op := range ops {
		ext := p.extensions[*op.OP]
		if err := ext.Apply(p, o, op); err != nil {

			if !p.StrictPathExists && errors.Is(err, ErrNotExists) {
				continue
			}
			v, ok := ext.(Descriptor)
			var desc string
			if ok {
				desc = v.Description(p, op)
			} else {
				desc = fmt.Sprintf("%s %s", *op.OP, *op.Path)
			}
			if errors.Is(err, ErrStop) {
				return fmt.Errorf("operation stopped: %s ext=%T, err=%w", desc, ext, err)
			}
			return fmt.Errorf("operation failed: %s ext=%T, err=%w", desc, ext, err)
		}
	}
	return nil
}

type addExtension struct{}

func (addExtension) OP() string {
	return opAdd
}

func (addExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path  = *op.Path
		value = *op.Value
		parts = NewJSONPointer(path)
	)
	if parts.IsTheWholeDocument() {
		*o = value
		return nil
	}
	parent, set, err := p.VisitPath(o, parts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", path, err)
	}
	return p.AddValue(parent, set, parts.LastToken(), value)
}

func (addExtension) Check(_ *Patch, op Operation) error {
	if op.Value == nil {
		return errors.New("operation add must contains a value member")
	}
	return nil
}

type removeExtension struct{}

func (removeExtension) OP() string {
	return opRemove
}

func (removeExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path  = *op.Path
		parts = NewJSONPointer(path)
	)
	parent, set, err := p.VisitPath(o, parts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", path, err)
	}
	return p.RemoveValue(parent, set, parts.LastToken())
}

func (removeExtension) Check(_ *Patch, _ Operation) error {
	return nil
}

type replaceExtension struct{}

func (replaceExtension) OP() string {
	return opReplace
}

func (replaceExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path  = *op.Path
		value = *op.Value
		parts = NewJSONPointer(path)
	)
	if parts.IsTheWholeDocument() {
		*o = value
		return nil
	}
	parent, set, err := p.VisitPath(o, parts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", path, err)
	}
	return p.ReplaceValue(parent, set, parts.LastToken(), value)
}

func (replaceExtension) Check(_ *Patch, op Operation) error {
	if op.Value == nil {
		return errors.New("operation replace must contains a value member")
	}
	return nil
}

type moveExtension struct{}

func (moveExtension) OP() string {
	return opMove
}

func (moveExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path      = *op.Path
		from      = *op.From
		parts     = NewJSONPointer(path)
		fromParts = NewJSONPointer(from)
	)
	fromParent, fromSet, err := p.VisitPath(o, fromParts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", from, err)
	}
	value, _, err := p.visitPathPart(fromParent, fromParts.LastToken())
	if err != nil {
		if p.StrictPathExists {
			return fmt.Errorf("path not exists: %s, err=%w", from, err)
		}
		return nil
	}
	if parts.SameParent(fromParts) {
		return p.MoveValue(fromParent, fromSet, fromParts.LastToken(), parts.LastToken())
	}
	if err := p.RemoveValue(fromParent, fromSet, fromParts.LastToken()); err != nil {
		return err
	}
	parent, set, err := p.VisitPath(o, parts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", path, err)
	}
	err = p.AddValue(parent, set, parts.LastToken(), value)
	return err
}

func (moveExtension) Check(_ *Patch, op Operation) error {
	if op.From == nil {
		return errors.New("operation move must contains a from member")
	}
	return nil
}

func (moveExtension) Description(_ *Patch, op Operation) string {
	return fmt.Sprintf("move %s to %s", *op.From, *op.Path)
}

type copyExtension struct{}

func (copyExtension) OP() string {
	return opCopy
}

func (copyExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path      = *op.Path
		from      = *op.From
		parts     = NewJSONPointer(path)
		fromParts = NewJSONPointer(from)
	)
	parent, set, err := p.VisitPath(o, parts.ParentPath()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", path, err)
	}
	value, _, err := p.VisitPath(o, fromParts.Path()...)
	if err != nil {
		return fmt.Errorf("path not exists: %s, err=%w", from, err)
	}
	return p.AddValue(parent, set, parts.LastToken(), deepCopy(value))
}

func (copyExtension) Check(_ *Patch, op Operation) error {
	if op.From == nil {
		return errors.New("operation copy must contains a from member")
	}
	return nil
}

func (copyExtension) Description(_ *Patch, op Operation) string {
	return fmt.Sprintf("copy %s from %s", *op.Path, *op.From)
}

type testExtension struct{}

func (testExtension) OP() string {
	return opTest
}

func (testExtension) Apply(p *Patch, o *any, op Operation) error {
	var (
		path   = *op.Path
		expect = *op.Value
		parts  = NewJSONPointer(path)
	)
	value, _, err := p.VisitPath(o, parts.Path()...)
	if err != nil {
		if p.StrictPathExists {
			return fmt.Errorf("path not exists: %s, err=%w", path, err)
		}
		return ErrStop
	}
	if reflect.DeepEqual(value, expect) {
		return nil
	}
	return ErrStop
}

func (testExtension) Check(_ *Patch, op Operation) error {
	if op.Value == nil {
		return errors.New("operation test must contains a value member")
	}
	return nil
}

/*
Evaluation of each reference token begins by decoding any escaped
character sequence.  This is performed by first transforming any
occurrence of the sequence '~1' to '/', and then transforming any
occurrence of the sequence '~0' to '~'.  By performing the
substitutions in this order, an implementation avoids the error of
turning '~01' first into '~1' and then into '/', which would be
incorrect (the string '~01' correctly becomes '~1' after
transformation).
*/
var unescapeReplace = strings.NewReplacer("~1", "/", "~0", "~")

func unescapePath(path string) string {
	return unescapeReplace.Replace(path)
}

func deepCopy(o any) any {
	switch v := o.(type) {
	case []any:
		c := make([]any, len(v))
		for i, v := range v {
			c[i] = deepCopy(v)
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(v))
		for k, v := range v {
			c[k] = deepCopy(v)
		}
		return c
	default:
		return o
	}
}

// sliceRemove removes the element at index i
func sliceRemove(s []any, i int) []any {
	return append(s[:i], s[i+1:]...)
}

// sliceInsert inserts v into s at index i.
// It exactly shifts the existing elements to the right.
func sliceInsert(s []any, i int, v any) []any {
	if i == len(s) {
		return append(s, v)
	}
	n := make([]any, len(s)+1)
	copy(n[:i], s[:i])
	n[i] = v
	copy(n[i+1:], s[i:])
	return n
}
