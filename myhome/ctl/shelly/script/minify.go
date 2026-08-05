package script

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	embeddedscripts "github.com/asnowfix/home-automation/internal/shelly/scripts"
	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/spf13/cobra"
)

// minify/demangle are local, offline tools: they read a script (embedded or
// from a local file), minify it, and optionally write out the minified code
// and/or a symbol map -- no MQTT, no device, no network. This matters
// because this feature is explicitly opt-in/exploratory (see
// docs/configuration.md "Scripts (JS minifier)" and CLAUDE.md's "Default
// OFF" requirement); a developer evaluating the esbuild engine should be
// able to do so entirely offline before ever touching daemon config or a
// real device. All business logic (engine selection, wrapping, symbol map
// derivation) lives in pkg/shelly/script and internal/shelly/scripts --
// per the Three-Tier Layer Rule in CLAUDE.md, this file is CLI plumbing
// only.

var (
	minifyEngine                string
	minifyMangleTopLevel        bool
	minifyPreserveGlobals       []string
	minifyDebugExports          []string
	minifyOutPath               string
	minifySymbolMapPath         string
	minifyDisableSyntaxLowering bool
)

func init() {
	Cmd.AddCommand(minifyCtl)
	minifyCtl.Flags().StringVar(&minifyEngine, "engine", string(pkgscript.EngineTdewolff), "Minifier engine: tdewolff (default) or esbuild")
	minifyCtl.Flags().BoolVar(&minifyMangleTopLevel, "mangle-top-level", false, "Also mangle top-level identifiers (esbuild engine only)")
	minifyCtl.Flags().StringSliceVar(&minifyPreserveGlobals, "preserve", nil, "Extra top-level names to keep reachable as globals, on top of this script's registered preserve-list (see internal/shelly/scripts/minify_config.go)")
	minifyCtl.Flags().StringSliceVar(&minifyDebugExports, "debug-export", nil, "Top-level names to keep reachable for live Script.Eval debugging, on top of this script's registered debug-export list")
	minifyCtl.Flags().StringVar(&minifyOutPath, "out", "", "Write the minified code to this file")
	minifyCtl.Flags().StringVar(&minifySymbolMapPath, "symbol-map", "", "Write the top-level symbol map (JSON) to this file (requires --engine esbuild --mangle-top-level)")
	minifyCtl.Flags().BoolVar(&minifyDisableSyntaxLowering, "disable-syntax-lowering", false, "esbuild engine only: keep sequential statements separate instead of collapsing them into comma-expressions -- see issue #266 (comma-expression-in-return crashed a real device with \"Too much recursion\"); costs a little size, NOT verified on hardware")
}

var minifyCtl = &cobra.Command{
	Use:   "minify SCRIPT",
	Short: "Minify a script locally (no device involved) and report its size",
	Long: `Minify SCRIPT -- either the basename of an embedded script (e.g. pool-pump.js) or a
path to a local .js file -- and print its minified size. This command never touches a device
or the network; it is a local, offline way to evaluate minifier engines and options before
changing daemon config.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return doMinifyCommand(args[0])
	},
}

// loadScriptSource resolves nameOrPath first against the embedded scripts
// filesystem (so "pool-pump.js" just works), falling back to a local file
// on disk. Returns the basename to use for registry lookups and error
// messages, and the raw source bytes.
func loadScriptSource(nameOrPath string) (name string, src []byte, err error) {
	if data, ferr := fs.ReadFile(embeddedscripts.GetFS(), nameOrPath); ferr == nil {
		return nameOrPath, data, nil
	}
	data, ferr := os.ReadFile(nameOrPath)
	if ferr != nil {
		return "", nil, fmt.Errorf("could not read %q as an embedded script or a local file: %w", nameOrPath, ferr)
	}
	return filepath.Base(nameOrPath), data, nil
}

func doMinifyCommand(nameOrPath string) error {
	name, src, err := loadScriptSource(nameOrPath)
	if err != nil {
		return err
	}

	engine := pkgscript.Engine(minifyEngine)
	opts := embeddedscripts.MinifyOptionsFor(name, engine, minifyMangleTopLevel, len(minifyDebugExports) > 0)
	opts.PreserveGlobals = append(opts.PreserveGlobals, minifyPreserveGlobals...)
	opts.DebugExports = append(opts.DebugExports, minifyDebugExports...)
	opts.DisableSyntaxLowering = minifyDisableSyntaxLowering

	res, err := pkgscript.MinifyWithOptions(name, src, opts)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %d bytes -> %d bytes (engine=%s mangle_top_level=%v)\n", name, len(src), len(res.Code), engine, minifyMangleTopLevel)

	if minifyOutPath != "" {
		if err := os.WriteFile(minifyOutPath, res.Code, 0o644); err != nil {
			return fmt.Errorf("write minified output: %w", err)
		}
		fmt.Printf("wrote minified code to %s\n", minifyOutPath)
	}

	if minifySymbolMapPath != "" {
		if res.Symbols == nil {
			return fmt.Errorf("no symbol map produced -- pass --engine esbuild --mangle-top-level to generate one")
		}
		if err := pkgscript.WriteSymbolMap(minifySymbolMapPath, res.Symbols); err != nil {
			return err
		}
		fmt.Printf("wrote symbol map (%d symbols) to %s\n", len(res.Symbols.Symbols), minifySymbolMapPath)
	}

	return nil
}
