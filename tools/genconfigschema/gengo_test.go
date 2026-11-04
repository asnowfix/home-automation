package main

import (
	"strings"
	"testing"
)

func TestGenerateGo_KVSKeysOnly(t *testing.T) {
	s := testSchema()
	src, err := GenerateGo(s, GoOptions{Package: "script", SourceJSON: "x.schema.json", KVSKeys: true})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if !strings.Contains(src, "package script") {
		t.Errorf("missing package clause:\n%s", src)
	}
	if !containsAll(src, "var XKeys = map[string]string{", `"enable_logging": "script/x/logging"`, `"count": "script/x/count"`) {
		t.Errorf("missing expected KVS key entries:\n%s", src)
	}
	if strings.Contains(src, "const (") {
		t.Errorf("KVSKeys-only mode must not emit Default consts:\n%s", src)
	}
	if _, err := formatGo(src); err != nil {
		t.Errorf("generated source is not valid Go: %v\n%s", err, src)
	}
}

func TestGenerateGo_ConstsOnly(t *testing.T) {
	s := testSchema()
	src, err := GenerateGo(s, GoOptions{Package: "pool", SourceJSON: "x.schema.json", Consts: true})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if !containsAll(src, "const (", "DefaultCount = 3") {
		t.Errorf("missing expected Default const:\n%s", src)
	}
	if strings.Contains(src, "map[string]string") {
		t.Errorf("Consts-only mode must not emit KVS key maps:\n%s", src)
	}
	if _, err := formatGo(src); err != nil {
		t.Errorf("generated source is not valid Go: %v\n%s", err, src)
	}
}

func TestGenerateGo_TimeDurationConst(t *testing.T) {
	s := &Schema{
		Script:     "x.js",
		KVSPrefix:  "script/x/",
		KVSKeysVar: "XKeys",
		Fields: []Field{
			{Name: "graceDelayMs", Key: "grace-delay", Default: 10000.0, Type: "number", GoConst: true, GoConstName: "GraceDelay", GoConstType: "time.Duration"},
		},
	}
	src, err := GenerateGo(s, GoOptions{Package: "pool", SourceJSON: "x.schema.json", Consts: true})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if !containsAll(src, `import "time"`, "DefaultGraceDelay = 10000 * time.Millisecond") {
		t.Errorf("missing expected time.Duration const:\n%s", src)
	}
	if _, err := formatGo(src); err != nil {
		t.Errorf("generated source is not valid Go: %v\n%s", err, src)
	}
}

func TestGenerateGo_FloatConstIsExplicitlyTyped(t *testing.T) {
	s := &Schema{
		Script:     "garden.js",
		KVSPrefix:  "script/garden/",
		KVSKeysVar: "GardenKVSKeys",
		Fields: []Field{
			{Name: "lunchStart", Key: "lunch-start", Default: 12.0, Type: "number", GoConst: true, GoConstType: "float64"},
		},
	}
	src, err := GenerateGo(s, GoOptions{Package: "garden", SourceJSON: "garden.schema.json", Consts: true})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	// Explicit float64 typing matters: these constants are passed directly to
	// fmt.Sprintf("%.1f", ...) as interface{} in myhome/ctl/garden/setup.go;
	// an untyped constant would default to `int` there and break formatting.
	if !strings.Contains(src, "DefaultLunchStart float64 = 12") {
		t.Errorf("expected explicitly float64-typed constant:\n%s", src)
	}
}

func TestGenerateGo_ZoneFieldKeys(t *testing.T) {
	s := &Schema{
		Script:           "garden.js",
		KVSPrefix:        "script/garden/",
		KVSKeysVar:       "GardenKVSKeys",
		ZoneFieldKeysVar: "ZoneFieldKeys",
		ZoneFields: []ZoneField{
			{Field: "appRateMmH", Key: "app-rate", Type: "number"},
		},
	}
	src, err := GenerateGo(s, GoOptions{Package: "garden", SourceJSON: "garden.schema.json", ZoneFieldKeys: true})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}
	if !containsAll(src, "var ZoneFieldKeys = map[string]string{", `"appRateMmH": "app-rate"`) {
		t.Errorf("missing expected zone field key entry:\n%s", src)
	}
}

func TestGoLiteral_TypeMismatchFails(t *testing.T) {
	_, err := goLiteral(Field{Name: "x", Default: true, Type: "number"})
	if err == nil {
		t.Fatal("expected error for bool default with number type")
	}
}
