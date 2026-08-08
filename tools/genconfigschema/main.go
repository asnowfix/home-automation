// Command genconfigschema generates, from a single JSON schema per Shelly
// script, both:
//   - the Go KVS-key maps and Default* constants that used to be
//     hand-maintained (PoolKVSKeys) or regex-scraped from the .js source
//     (tools/extract-pool-defaults, tools/extract-garden-defaults), and
//   - the CONFIG_SCHEMA (and, for garden.js, ZONE_KEY_SPECS) block injected
//     in place into the .js file, between generated-block markers.
//
// See GitHub issue #439.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	schemaPath := flag.String("schema", "", "path to the *.schema.json source of truth (required)")
	jsPath := flag.String("js", "", "path to the .js file whose CONFIG_SCHEMA block(s) to regenerate in place")
	goPath := flag.String("go", "", "path to the Go file to (re)generate")
	goPackage := flag.String("go-package", "", "Go package name for -go's output (required if -go is set)")
	consts := flag.Bool("consts", false, "emit Default* constants into -go")
	kvsKeys := flag.Bool("kvskeys", false, "emit the schema's KVSKeysVar map into -go")
	zoneFieldKeys := flag.Bool("zonefieldkeys", false, "emit the schema's ZoneFieldKeysVar map into -go")
	flag.Parse()

	if *schemaPath == "" {
		fmt.Fprintln(os.Stderr, "genconfigschema: -schema is required")
		os.Exit(1)
	}

	schema, err := LoadSchema(*schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genconfigschema: %v\n", err)
		os.Exit(1)
	}

	if *jsPath != "" {
		if err := regenerateJS(schema, *jsPath); err != nil {
			fmt.Fprintf(os.Stderr, "genconfigschema: regenerating %s: %v\n", *jsPath, err)
			os.Exit(1)
		}
		fmt.Printf("genconfigschema: regenerated CONFIG_SCHEMA block(s) in %s from %s\n", *jsPath, *schemaPath)
	}

	if *goPath != "" {
		if *goPackage == "" {
			fmt.Fprintln(os.Stderr, "genconfigschema: -go-package is required when -go is set")
			os.Exit(1)
		}
		src, err := GenerateGo(schema, GoOptions{
			Package:       *goPackage,
			SourceJSON:    *schemaPath,
			Consts:        *consts,
			KVSKeys:       *kvsKeys,
			ZoneFieldKeys: *zoneFieldKeys,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "genconfigschema: generating Go: %v\n", err)
			os.Exit(1)
		}
		formatted, err := formatGo(src)
		if err != nil {
			// Fall back to the unformatted source so the compiler error
			// points at the real problem, but still fail the run.
			os.WriteFile(*goPath, []byte(src), 0o644)
			fmt.Fprintf(os.Stderr, "genconfigschema: gofmt failed (wrote unformatted %s for inspection): %v\n", *goPath, err)
			os.Exit(1)
		}
		if err := os.WriteFile(*goPath, []byte(formatted), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "genconfigschema: writing %s: %v\n", *goPath, err)
			os.Exit(1)
		}
		fmt.Printf("genconfigschema: wrote %s from %s\n", *goPath, *schemaPath)
	}
}

// regenerateJS rewrites the CONFIG_SCHEMA block (and ZONE_KEY_SPECS block,
// if the schema has zoneFields) in place inside the file at jsPath.
func regenerateJS(schema *Schema, jsPath string) error {
	buf, err := os.ReadFile(jsPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", jsPath, err)
	}
	src := string(buf)

	configBlock, err := GenerateJSConfigSchema(schema)
	if err != nil {
		return fmt.Errorf("generating CONFIG_SCHEMA: %w", err)
	}
	src, err = InjectBlock(src, "CONFIG_SCHEMA", configBlock)
	if err != nil {
		return fmt.Errorf("injecting CONFIG_SCHEMA: %w", err)
	}

	if len(schema.ZoneFields) > 0 {
		zoneBlock, err := GenerateJSZoneKeySpecs(schema)
		if err != nil {
			return fmt.Errorf("generating ZONE_KEY_SPECS: %w", err)
		}
		src, err = InjectBlock(src, "ZONE_KEY_SPECS", zoneBlock)
		if err != nil {
			return fmt.Errorf("injecting ZONE_KEY_SPECS: %w", err)
		}
	}

	return os.WriteFile(jsPath, []byte(src), 0o644)
}
