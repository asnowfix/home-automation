package main

import (
	"devices/shelly"
	"fmt"
	"os"
)

func main() {
	devices, err := shelly.MyShellies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	} else {
		fmt.Printf("Devices: %v", devices)
	}
}
