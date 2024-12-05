// Copyright (c) 2024 hanke. All rights reserved.
// Use of this source code is governed by a MIT license
// that can be found in the LICENSE file.
package jsonpatch

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSliceInsert(t *testing.T) {
	cases := []struct {
		doc    []any
		expect []any
		i      int
		v      int
	}{
		{
			doc:    []any{1, 2, 3},
			expect: []any{1, 2, 4, 3},
			i:      2,
			v:      4,
		},
		{
			doc:    []any{1, 2, 3},
			expect: []any{1, 2, 3, 4},
			i:      3,
			v:      4,
		},
		{
			doc:    []any{1, 2, 3},
			expect: []any{1, 4, 2, 3},
			i:      1,
			v:      4,
		},
		{
			doc:    []any{},
			expect: []any{1},
			i:      0,
			v:      1,
		},
	}
	for _, c := range cases {
		got := sliceInsert(c.doc, c.i, c.v)
		if !reflect.DeepEqual(got, c.expect) {
			t.Fatal("expected", c.expect, "got", got)
		}
	}
}

func TestOperationAdd(t *testing.T) {
	j := `{"op":"add","path":"/foo","value":null}`
	var o Operation
	err := json.Unmarshal([]byte(j), &o)
	if err != nil {
		t.Fatal(err)
	}
	if *o.OP != "add" {
		t.Fatal("expected 'add', got", o.OP)
	}
	if *o.Path != "/foo" {
		t.Fatal("expected '/foo', got", o.Path)
	}
	if *o.Value != nil {
		t.Fatal("expected nil, got", o.Value)
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != j {
		t.Fatal("expected", j, "got", string(b))
	}
}

func TestOperationRemove(t *testing.T) {
	j := `{"op":"remove","path":"/foo"}`
	var o Operation
	err := json.Unmarshal([]byte(j), &o)
	if err != nil {
		t.Fatal(err)
	}
	if *o.OP != "remove" {
		t.Fatal("expected 'remove', got", *o.OP)
	}
	if *o.Path != "/foo" {
		t.Fatal("expected '/foo', got", *o.Path)
	}
	if o.Value != nil {
		t.Fatal("expected nil, got", o.Value)
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != j {
		t.Fatal("expected", j, "got", string(b))
	}
}

func TestOperationCopy(t *testing.T) {
	j := `{"op":"copy","path":"/foo","from":"/bar"}`
	var o Operation
	err := json.Unmarshal([]byte(j), &o)
	if err != nil {
		t.Fatal(err)
	}
	if *o.OP != "copy" {
		t.Fatal("expected 'copy', got", *o.OP)
	}
	if *o.Path != "/foo" {
		t.Fatal("expected '/foo', got", *o.Path)
	}
	if o.Value != nil {
		t.Fatal("expected nil, got", o.Value)
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != j {
		t.Fatal("expected", j, "got", string(b))
	}
}

type testCase struct {
	Comment  string      `json:"comment"`
	Doc      interface{} `json:"doc"`
	Patch    []Operation `json:"patch"`
	Expected interface{} `json:"expected"`
	Error    string      `json:"error"`
	Disabled bool        `json:"disabled"`
	Options  []string    `json:"options"`
}

func (tc *testCase) New(t *testing.T) *Patch {
	var opts []Option
	for _, v := range tc.Options {
		switch v {
		case "NoStrictPathExists":
			opts = append(opts, WithStrictPathExists(false))
		case "SupportNegativeArrayIndex":
			opts = append(opts, WithSupportNegativeArrayIndex(true))
		default:
			t.Fatal("unknown option", v)
		}
	}
	return New(opts...)
}

func jsonstring(o interface{}) string {
	var s strings.Builder
	enc := json.NewEncoder(&s)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(o)
	return s.String()
}

func testFile(t *testing.T, filename string) {
	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var tests []testCase
	if err := json.Unmarshal(b, &tests); err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		if test.Disabled {
			continue
		}
		test := test
		t.Run(test.Comment, func(t *testing.T) {
			err := test.New(t).ApplyAny(&test.Doc, test.Patch)
			if err != nil {
				if test.Error == "" {
					t.Fatal(err)
				}
				return
			}
			if test.Error != "" {
				t.Fatal("expected error", test.Error)
				return
			}
			if !reflect.DeepEqual(test.Expected, test.Doc) {
				t.Fatal("expected", jsonstring(test.Expected), "got", jsonstring(test.Doc))
			}
		})
	}
}

func TestSpec(t *testing.T) {
	testFile(t, "json-patch-tests/spec_tests.json")
}

func TestTestsJSON(t *testing.T) {
	testFile(t, "json-patch-tests/tests.json")
}

func TestExtend(t *testing.T) {
	testFile(t, "tests.json")
}
