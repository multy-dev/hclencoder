package hclencoder

import (
	"fmt"
	"github.com/zclconf/go-cty/cty"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

type encoderTest2 struct {
	ID     string
	Input  interface{}
	Output string
	Error  bool
}

func TestEncoder(t *testing.T) {
	tests := []encoderTest2{
		{
			ID:     "empty struct",
			Input:  struct{}{},
			Output: "empty",
		},
		{
			ID: "basic struct",
			Input: struct {
				String        string
				EscapedString string
				Int           int
				Bool          bool
				Float         float64
			}{
				"bar",
				`"\
	`,
				123,
				true,
				4.56,
			},
			Output: "basic",
		},
		{
			ID: "cty value",
			Input: struct {
				String             cty.Value
				Map                cty.Value
				Slice              cty.Value
				TemplateExpression cty.Value
			}{
				String: cty.StringVal("test"),
				Map: cty.ObjectVal(map[string]cty.Value{
					"outer": cty.MapVal(map[string]cty.Value{
						"inner": cty.NumberIntVal(5),
					}),
				}),
				Slice: cty.TupleVal([]cty.Value{
					cty.ListVal([]cty.Value{cty.StringVal("foo")}),
					cty.StringVal("bar"),
					cty.NullVal(cty.String),
				}),
				TemplateExpression: cty.StringVal("${func(\"str\")}\n"),
			},
			Output: "cty-value",
		},
		{
			ID: "escaped strings",
			Input: struct {
				EscapedString  string
				TemplateString string `hcl:",expr"`
			}{
				"\n\t\r\\\"",
				"\"test-\u0041${\"\\\\ \\\"\"}\"",
			},
			Output: "escaped-strings",
		},
		{
			ID: "basic struct with expression",
			Input: struct {
				String string `hcl:",expr"`
			}{
				"bar",
			},
			Output: "basic-expr",
		},
		{
			ID: "labels changed",
			Input: struct {
				String string `hcl:"foo"`
				Int    int    `hcl:"baz"`
			}{
				"bar",
				123,
			},
			Output: "label-change",
		},
		{
			ID: "primitive list",
			Input: struct {
				Widgets []string
				Gizmos  []int
				Single  []string
			}{
				[]string{"foo", "bar", "baz"},
				[]int{4, 5, 6},
				[]string{"foo"},
			},
			Output: "primitive-lists",
		},
		{
			ID: "expression slice",
			Input: struct {
				Expressions []string `hcl:",expr"`
			}{
				[]string{"file(\"foo\")", "bar", "baz"},
			},
			Output: "expr-slices",
		},
		{
			ID: "repeated blocks",
			Input: struct {
				Widget []struct{} `hcl:",blocks"`
			}{
				[]struct{}{{}, {}},
			},
			Output: "repeated-blocks",
		},
		{
			ID: "nested struct",
			Input: struct {
				Foo  struct{ Bar string }
				Fizz struct{ Buzz float64 }
			}{
				struct{ Bar string }{Bar: "baz"},
				struct{ Buzz float64 }{Buzz: 1.23},
			},
			Output: "nested-structs",
		},
		{
			ID: "keyed nested struct",
			Input: struct {
				Foo struct {
					Key  string `hcl:",key"`
					Fizz string
				}
			}{
				struct {
					Key  string `hcl:",key"`
					Fizz string
				}{
					"bar",
					"buzz",
				},
			},
			Output: "keyed-nested-structs",
		},
		{
			ID: "multiple keys nested structs",
			Input: struct {
				Foo struct {
					Key      string `hcl:",key"`
					OtherKey string `hcl:",key"`
					Fizz     string
				}
			}{
				struct {
					Key      string `hcl:",key"`
					OtherKey string `hcl:",key"`
					Fizz     string
				}{
					"bar",
					"baz",
					"buzz",
				},
			},
			Output: "multiple-keys-nested-structs",
		},
		{
			ID: "nested struct slice",
			Input: struct {
				Widget []struct {
					Foo string `hcl:"foo,key"`
				} `hcl:",blocks"`
			}{
				[]struct {
					Foo string `hcl:"foo,key"`
				}{
					{"bar"},
					{"baz"},
				},
			},
			Output: "nested-struct-slice",
		},
		{
			ID: "nested struct slice no key",
			Input: struct {
				Widget []struct {
					Foo string
				}
			}{
				Widget: []struct {
					Foo string
				}{
					{"bar"},
					{"baz"},
				},
			},
			Output: "nested-struct-slice-no-key",
		},
		{
			ID: "maps",
			Input: struct {
				Foo struct {
					KeyVals map[string]string
				}
			}{
				struct {
					KeyVals map[string]string
				}{
					KeyVals: map[string]string{
						"baz": "buzz",
					},
				},
			},
			Output: "maps",
		},
		{
			ID: "nested slices",
			Input: struct {
				Value map[string]interface{}
			}{Value: map[string]interface{}{
				"foo": []interface{}{
					"bar", "baz",
				},
				"bar": []interface{}{
					[]interface{}{
						"bar",
					},
					[]interface{}{
						"baz",
					},
					[]interface{}{
						"buzz",
					},
				},
			}},
			Output: "nested-slices",
		},
	}

	for _, test := range tests {
		actual, err := Encode(test.Input)
		if err != nil {
			t.Error(err)
		}

		if test.Error {
			assert.Error(t, err, test.ID)
		} else {
			expected, ferr := ioutil.ReadFile(fmt.Sprintf("_tests/%s.hcl", test.Output))
			if ferr != nil {
				t.Fatal(test.ID, "- could not read output HCL: ", ferr)
				continue
			}

			assert.NoError(t, err, test.ID)
			assert.EqualValues(
				t,
				string(expected),
				string(actual),
				fmt.Sprintf("%s\nExpected:\n%s\nActual:\n%s", test.ID, expected, actual),
			)
		}
	}
}
