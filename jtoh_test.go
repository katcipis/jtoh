package jtoh_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/katcipis/jtoh"
)

// TODO:
// JSON stream that has a list inside
// JSON List that has a list inside
// Non JSON data mixed with JSON data (stream of JSONs with sometimes something that is not JSON)
// Newlines on values are removed (or else we don't have single line stream of logs anymore)

func TestTransform(t *testing.T) {
	type Test struct {
		name     string
		selector string
		input    []string
		output   []string
		wantErr  error
	}

	tests := []Test{
		{
			name:     "ErrOnEmptySelector",
			selector: "",
			wantErr:  jtoh.InvalidSelectorErr,
		},
		{
			name:     "ErrOnSelectorWithOnlySeparator",
			selector: ":",
			wantErr:  jtoh.InvalidSelectorErr,
		},
		{
			name:     "ErrOnSelectorWithOnlyNonASCIISeparator",
			selector: "λ",
			wantErr:  jtoh.InvalidSelectorErr,
		},
		{
			name:     "EmptyInput",
			selector: ":field",
			input:    []string{},
			output:   []string{},
		},
		{
			name:     "SingleSelectStringField",
			selector: ":string",
			input:    []string{`{"string":"lala"}`},
			output:   []string{"lala"},
		},
		{
			name:     "SingleSelectStringFieldWithNonASCIISeparator",
			selector: "λfieldλfieldb",
			input:    []string{`{"field":"lala","fieldb":5}`},
			output:   []string{"lalaλ5"},
		},
		{
			name:     "SingleSelectNumberField",
			selector: ":number",
			input:    []string{`{"number":666}`},
			output:   []string{"666"},
		},
		{
			name:     "SingleSelectBoolField",
			selector: ":bool",
			input:    []string{`{"bool":true}`},
			output:   []string{"true"},
		},
		{
			name:     "SingleSelectNullField",
			selector: ":null",
			input:    []string{`{"null":null}`},
			output:   []string{"<nil>"},
		},
		{
			name:     "SingleSelectMultipleObjs",
			selector: ":string",
			input: []string{
				`{"string":"one"}`,
				`{"string":"two"}`,
			},
			output: []string{
				"one",
				"two",
			},
		},
		{
			name:     "SingleNestedSelectStringField",
			selector: ":nested.string",
			input:    []string{`{"nested" : { "string":"lala"} }`},
			output:   []string{"lala"},
		},
		{
			name:     "SingleNestedSelectNumberField",
			selector: ":nested.number",
			input:    []string{`{"nested" : { "number":13} }`},
			output:   []string{"13"},
		},
		{
			name:     "MultipleSelectedFields",
			selector: ":string:number:bool",
			input:    []string{`{"string":"hi","number":7,"bool":false}`},
			output:   []string{"hi:7:false"},
		},
		{
			name:     "MultipleSelectedFieldsMultipleObjs",
			selector: ":string:number:bool",
			input: []string{
				`{"string":"hi","number":7,"bool":false}`,
				`{"number":6.6,"bool":true,"string":"katz"}`,
			},
			output: []string{
				"hi:7:false",
				"katz:6.6:true",
			},
		},
		{
			name:     "MultipleSelectedFieldsWithOneMissing",
			selector: ":string:number:missing:bool",
			input:    []string{`{"string":"hi","number":7,"bool":false}`},
			output:   []string{fmt.Sprintf("hi:7:%s:false", missingFieldErrMsg("missing"))},
		},
		{
			name:     "IncompletePathToField",
			selector: ":nested.number",
			input:    []string{`{"nested" : {} }`},
			output:   []string{missingFieldErrMsg("nested.number")},
		},
		{
			name:     "PathToFieldWithWrongType",
			selector: ":nested.number",
			input:    []string{`{"nested" : "notObj" }`},
			output:   []string{missingFieldErrMsg("nested.number")},
		},
		{
			name:     "UnselectedFieldIsIgnored",
			selector: ":number",
			input:    []string{`{"number":666,"ignored":"hi"}`},
			output:   []string{"666"},
		},
		{
			name:     "MissingField",
			selector: ":missing",
			input:    []string{`{"number":666,"ignored":"hi"}`},
			output:   []string{missingFieldErrMsg("missing")},
		},
		{
			name:     "IgnoreSpacesOnBeginning",
			selector: ":string",
			input:    []string{` {"string":"lala"}`},
			output:   []string{"lala"},
		},
		{
			name:     "IgnoreTabsOnBeginning",
			selector: ":string",
			input: []string{`	{"string":"lala"}`},
			output: []string{"lala"},
		},
		{
			name:     "IgnoreNewlinesOnBeginning",
			selector: ":string",
			input: []string{`
				{"string":"lala"}`,
			},
			output: []string{"lala"},
		},
		{
			// Not entirely sure that trimming is the way to go in this case
			// But it seems pretty odd to have a json key with trailing spaces
			// And at the same time it would be valid JSON
			name:     "FieldAccessorTrailingSpacesAreTrimmed",
			selector: ": field :  field2  ",
			input:    []string{`{"field":666, "field2":"lala"}`},
			output:   []string{"666:lala"},
		},
		{
			name:     "FieldAccessorCanHaveSpaces",
			selector: ": field with space : field ",
			input:    []string{`{"field with space":666, "field":"stonks"}`},
			output:   []string{"666:stonks"},
		},
		{
			name:     "NestedFieldAccessorCanHaveSpaces",
			selector: ": nested field.field with space : field ",
			input:    []string{`{"nested field" : { "field with space":666 }, "field":"stonks"}`},
			output:   []string{"666:stonks"},
		},
		{
			name:     "TrailingNewlinesOnValuesAreTrimmed",
			selector: ":field",
			input: []string{
				"{\"field\":\"\\nvalue1\\n\\n\"}",
				"{\"field\":\"\\nvalue2\"}",
			},
			output: []string{"value1", "value2"},
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name+"WithList", func(t *testing.T) {
			input := strings.NewReader("[" + strings.Join(test.input, ",") + "]")
			testTransform(t, input, test.selector, test.output, test.wantErr)
		})

		t.Run(test.name+"WithStream", func(t *testing.T) {
			input := strings.NewReader(strings.Join(test.input, "\n"))
			testTransform(t, input, test.selector, test.output, test.wantErr)
		})
	}
}

func testTransform(
	t *testing.T,
	input io.Reader,
	selector string,
	want []string,
	wantErr error,
) {
	t.Helper()

	j, err := jtoh.New(selector)

	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Errorf("got err[%v] want[%v]", err, wantErr)
		}
		return
	}

	if err != nil {
		t.Errorf("unexpected error [%v]", err)
		return
	}

	output := bytes.Buffer{}

	j.Do(input, &output)

	gotLines := bufio.NewScanner(&output)
	lineCount := 0

	for gotLines.Scan() {
		gotLine := gotLines.Text()
		if lineCount >= len(want) {
			t.Errorf("unexpected extra line: %q", gotLine)
			continue
		}
		wantLine := want[lineCount]
		if gotLine != wantLine {
			t.Errorf("line[%d]: got %q != want %q", lineCount, gotLine, wantLine)
		}
		lineCount += 1
	}

	if lineCount != len(want) {
		t.Errorf("got %d lines, want %d", lineCount, len(want))
	}

	if err := gotLines.Err(); err != nil {
		t.Errorf("unexpected error scanning output lines: %v", err)
	}
}

func missingFieldErrMsg(selector string) string {
	return fmt.Sprintf("<jtoh:missing field %q>", selector)
}
