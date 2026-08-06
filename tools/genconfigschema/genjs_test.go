package main

import (
	"strings"
	"testing"
)

func testSchema() *Schema {
	return &Schema{
		Script:     "x.js",
		KVSPrefix:  "script/x/",
		KVSKeysVar: "XKeys",
		Fields: []Field{
			{Name: "enableLogging", Description: "Enable logging when true", Key: "logging", Default: true, Type: "boolean"},
			{Name: "count", Description: "How many", Key: "count", Default: 3.0, Type: "number", GoConst: true},
			{Name: "label", Key: "label", Default: "hi", Type: "string", CliOnly: true},
			{Name: "required1", Key: "req", Default: nil, Type: "string", Required: true},
		},
	}
}

func TestGenerateJSConfigSchema_NoDescriptionFields(t *testing.T) {
	block, err := GenerateJSConfigSchema(testSchema())
	if err != nil {
		t.Fatalf("GenerateJSConfigSchema: %v", err)
	}
	if strings.Contains(block, "description:") {
		t.Errorf("generated CONFIG_SCHEMA still contains a description: object field:\n%s", block)
	}
	if !strings.Contains(block, "// Enable logging when true") {
		t.Errorf("expected description as a // comment, got:\n%s", block)
	}
	if !containsAll(block, `key: "logging"`, "default: true", `type: "boolean"`) {
		t.Errorf("missing expected enableLogging fields:\n%s", block)
	}
	if !containsAll(block, `cliOnly: true`) {
		t.Errorf("missing cliOnly: true for label field:\n%s", block)
	}
	if !containsAll(block, `required: true`) {
		t.Errorf("missing required: true for required1 field:\n%s", block)
	}
	if !strings.Contains(block, "default: null") {
		t.Errorf("expected null default for required1, got:\n%s", block)
	}
}

func TestGenerateJSZoneKeySpecs(t *testing.T) {
	s := &Schema{
		Script:           "garden.js",
		KVSPrefix:        "script/garden/",
		KVSKeysVar:       "GardenKVSKeys",
		ZoneFieldKeysVar: "ZoneFieldKeys",
		ZoneFields: []ZoneField{
			{Field: "appRateMmH", Key: "app-rate", Type: "number"},
			{Field: "group", Key: "group", Type: "string"},
		},
	}
	block, err := GenerateJSZoneKeySpecs(s)
	if err != nil {
		t.Fatalf("GenerateJSZoneKeySpecs: %v", err)
	}
	if !containsAll(block, "var ZONE_KEY_SPECS = [", `field: "appRateMmH"`, `key: "app-rate"`, `field: "group"`) {
		t.Errorf("unexpected ZONE_KEY_SPECS block:\n%s", block)
	}
}

func TestInjectBlock_RoundTrip(t *testing.T) {
	src := "// header\n" +
		"var UNRELATED = 1;\n" +
		"// >>> GENERATED: CONFIG_SCHEMA (source: schema JSON; regenerate via `make generate` — DO NOT EDIT BY HAND) >>>\n" +
		"var CONFIG_SCHEMA = { old: true };\n" +
		"// <<< GENERATED: CONFIG_SCHEMA <<<\n" +
		"// footer\n"

	out, err := InjectBlock(src, "CONFIG_SCHEMA", "var CONFIG_SCHEMA = { newField: 1 };\n")
	if err != nil {
		t.Fatalf("InjectBlock: %v", err)
	}
	if strings.Contains(out, "old: true") {
		t.Errorf("old block content survived injection:\n%s", out)
	}
	if !strings.Contains(out, "newField: 1") {
		t.Errorf("new block content missing after injection:\n%s", out)
	}
	if !strings.Contains(out, "// header") || !strings.Contains(out, "// footer") || !strings.Contains(out, "var UNRELATED = 1;") {
		t.Errorf("content outside markers was not preserved:\n%s", out)
	}

	// Idempotency: injecting the same body twice must produce identical output
	// (round-tripping `go generate` twice in a row should be a no-op diff).
	out2, err := InjectBlock(out, "CONFIG_SCHEMA", "var CONFIG_SCHEMA = { newField: 1 };\n")
	if err != nil {
		t.Fatalf("InjectBlock (second pass): %v", err)
	}
	if out != out2 {
		t.Errorf("InjectBlock is not idempotent:\nfirst:\n%s\nsecond:\n%s", out, out2)
	}
}

func TestInjectBlock_MissingMarkerFails(t *testing.T) {
	_, err := InjectBlock("no markers here", "CONFIG_SCHEMA", "irrelevant")
	if err == nil {
		t.Fatal("expected error when the begin marker is missing")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
