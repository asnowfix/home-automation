package jstarget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return b
}

// wantRegion describes what a resolved Region must look like, checked via
// substring bounds rather than exact byte offsets so fixtures can be edited
// without recomputing offsets by hand.
type wantRegion struct {
	target       Target
	name         string
	textPrefix   string
	textSuffix   string
	textContains string
}

func checkRegions(t *testing.T, src []byte, got []Region, want []wantRegion) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d regions, want %d\ngot: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		r := got[i]
		if r.Target != w.target {
			t.Errorf("region[%d]: Target = %q, want %q", i, r.Target, w.target)
		}
		if r.Name != w.name {
			t.Errorf("region[%d]: Name = %q, want %q", i, r.Name, w.name)
		}
		if r.Start < 0 || r.End > len(src) || r.Start >= r.End {
			t.Fatalf("region[%d]: invalid byte range [%d,%d) for source of length %d", i, r.Start, r.End, len(src))
		}
		text := string(src[r.Start:r.End])
		if w.textPrefix != "" && !strings.HasPrefix(text, w.textPrefix) {
			t.Errorf("region[%d]: text = %q, want prefix %q", i, text, w.textPrefix)
		}
		if w.textSuffix != "" && !strings.HasSuffix(text, w.textSuffix) {
			t.Errorf("region[%d]: text = %q, want suffix %q", i, text, w.textSuffix)
		}
		if w.textContains != "" && !strings.Contains(text, w.textContains) {
			t.Errorf("region[%d]: text = %q, want to contain %q", i, text, w.textContains)
		}
	}
}

func TestRegions_ResolvesConstructs(t *testing.T) {
	tests := []struct {
		name string
		file string
		want []wantRegion
	}{
		{
			name: "function declaration",
			file: "function_declaration.js",
			want: []wantRegion{
				{target: TargetDaemon, name: "forecastTransform", textPrefix: "function forecastTransform(body) {", textSuffix: "}"},
			},
		},
		{
			name: "function expression assigned to var",
			file: "function_expression_var.js",
			want: []wantRegion{
				{target: TargetDaemon, name: "forecastTransform", textPrefix: "var forecastTransform = function (body) {", textSuffix: "};"},
			},
		},
		{
			name: "single var declaration",
			file: "var_declaration.js",
			want: []wantRegion{
				{target: TargetDevice, name: "deviceOnlyFlag", textPrefix: "var deviceOnlyFlag", textSuffix: ";"},
			},
		},
		{
			name: "multi-declarator var declaration is one region",
			file: "multi_declarator.js",
			want: []wantRegion{
				{target: TargetDaemon, name: "a, b", textPrefix: "var a = 1,", textSuffix: "b = 2;"},
			},
		},
		{
			name: "object property",
			file: "object_property.js",
			want: []wantRegion{
				{target: TargetDaemon, name: "transform", textPrefix: "transform:", textSuffix: "1"},
			},
		},
		{
			name: "multi-line object literal",
			file: "multiline_object_literal.js",
			want: []wantRegion{
				{
					target:       TargetDaemon,
					name:         "CONFIG",
					textPrefix:   "var CONFIG = {",
					textSuffix:   "};",
					textContains: "headers: {",
				},
			},
		},
		{
			name: "multiple independent regions, some nested-looking but not overlapping",
			file: "multiple_regions.js",
			want: []wantRegion{
				{target: TargetDaemon, name: "forecastTransform", textPrefix: "function forecastTransform"},
				{target: TargetDevice, name: "localOnly", textPrefix: "localOnly:"},
				{target: TargetBoth, name: "explicitBoth", textPrefix: "function explicitBoth"},
			},
		},
		{
			name: "no annotations at all yields zero regions",
			file: "no_annotations.js",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := readTestdata(t, tc.file)
			got, err := Regions(src)
			if err != nil {
				t.Fatalf("Regions(%s): unexpected error: %v", tc.file, err)
			}
			checkRegions(t, src, got, tc.want)
		})
	}
}

func TestRegions_Errors(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantErrText string
	}{
		{
			name:        "unrecognised target value is an error",
			file:        "unknown_target.js",
			wantErrText: `unrecognised @target value "demon"`,
		},
		{
			name:        "annotation with nothing after it is an error",
			file:        "trailing_comment.js",
			wantErrText: "does not annotate a recognised construct",
		},
		{
			name:        "annotation inside an expression is an error",
			file:        "not_a_construct.js",
			wantErrText: "does not annotate a recognised construct",
		},
		{
			name:        "nested annotations are an error",
			file:        "nested_annotation.js",
			wantErrText: "nested @target annotation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := readTestdata(t, tc.file)
			_, err := Regions(src)
			if err == nil {
				t.Fatalf("Regions(%s): expected an error, got none", tc.file)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("Regions(%s): error = %q, want it to contain %q", tc.file, err.Error(), tc.wantErrText)
			}
		})
	}
}

// TestRegions_PoolPumpHasNoAnnotations is the regression guard from #568: the
// parser must not misfire on the real, hand-tuned, ~3000-line production
// source, which today carries zero @target annotations.
func TestRegions_PoolPumpHasNoAnnotations(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "pool-pump.js"))
	if err != nil {
		t.Fatalf("reading pool-pump.js: %v", err)
	}
	regions, err := Regions(src)
	if err != nil {
		t.Fatalf("Regions(pool-pump.js): unexpected error: %v", err)
	}
	if len(regions) != 0 {
		t.Fatalf("Regions(pool-pump.js): got %d regions, want 0: %+v", len(regions), regions)
	}
}
