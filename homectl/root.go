package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"homectl/list"
	"homectl/show"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use: "homectl",
	Run: func(cmd *cobra.Command, args []string) {
		InitLog()
		if !Verbose {
			log.Default().SetOutput(io.Discard)
		}
	},
}

var Verbose bool

func InitLog() {
	if !Verbose {
		log.Default().SetOutput(io.Discard)
	} else {
		log.Default().Print("Turning on logging")
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose output")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(show.Cmd)
	rootCmd.AddCommand(list.Cmd)
}

var Commit string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	// Long:  `All software has versions. This is Hugo's`,
	Run: func(cmd *cobra.Command, args []string) {
		InitLog()
		fmt.Println(Commit)
	},
}
