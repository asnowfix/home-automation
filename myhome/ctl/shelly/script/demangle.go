package script

import (
	"fmt"
	"strings"

	pkgscript "github.com/asnowfix/home-automation/pkg/shelly/script"

	"github.com/spf13/cobra"
)

func init() {
	Cmd.AddCommand(demangleCtl)
}

var demangleCtl = &cobra.Command{
	Use:   "demangle SYMBOL_MAP_FILE MESSAGE...",
	Short: "Translate a Shelly crash message's mangled identifiers back to their original names",
	Long: `Reads a symbol map JSON file (written by "myhome ctl shelly script minify --symbol-map")
and substitutes every mangled top-level identifier found in MESSAGE with its original name.
MESSAGE may be given as several arguments (joined with spaces) so a crash message can be
pasted without quoting every special character.

All substitution logic lives in pkg/shelly/script.SymbolMap.Demangle -- this command is a
thin, offline wrapper around it; it never touches a device.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := pkgscript.ReadSymbolMap(args[0])
		if err != nil {
			return err
		}
		msg := strings.Join(args[1:], " ")
		fmt.Println(m.Demangle(msg))
		return nil
	},
}
