package main

import (
	"fmt"
	"github.com/asnowfix/home-automation/internal/myip"
)

func main() {
	ip := myip.SeeIp()
	fmt.Printf("IPv4: %v", ip)
}
