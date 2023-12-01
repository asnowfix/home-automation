package main

import (
	"fmt"

	arg "github.com/alexflint/go-arg"
)

func main() {
	var args struct {
		IP string `arg:"positional"`
		// Output  []string `arg:"positional"`
	}
	arg.MustParse(&args)
	fmt.Println("IP:", args.IP)
	// fmt.Println("Output:", args.Output)

}
