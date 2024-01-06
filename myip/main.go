package main

import (
	"fmt"
	"internal/myip"
)

func main() {
	ip := myip.SeeIp()
	fmt.Printf("IPv4: %v", ip)
}
