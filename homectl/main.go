package main

import (
	"devices/shelly"
	"encoding/json"
	"fmt"
	"net"

	arg "github.com/alexflint/go-arg"
)

func main() {
	var args struct {
		IP string `arg:"positional"`
		// Output  []string `arg:"positional"`
	}
	arg.MustParse(&args)
	devices, err := shelly.MyShellies(net.ParseIP(args.IP))
	if err != nil {
		panic(err)
	}
	out, err := json.Marshal((*devices)[args.IP])
	if err != nil {
		panic(err)
	}
	fmt.Print(string(out))
}
