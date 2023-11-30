package main

import (
	"devices/shelly"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	devices, err := shelly.MyShellies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	} else {
		out, err := json.Marshal(devices)
		if err != nil {
			panic(err)
		}
		// fmt.Printf("Found %v devices '%v'\n", len(devices), reflect.TypeOf(device))
		fmt.Printf(string(out))
	}
}
