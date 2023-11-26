package main

import (
	"devices/shelly"
	"fmt"
	"os"
	"reflect"
)

func main() {
	devices, err := shelly.MyShellies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	} else {
		fmt.Printf("Found %v devices\n", devices.Len())
		for di := devices.Front(); di != nil; di = di.Next() {
			device := di.Value.(shelly.Device)
			fmt.Printf("%s: %v\n", reflect.TypeOf(device), device)

		}
	}
}
