package main

import (
	"context"
	"fmt"
	"os"

	"github.com/asnowfix/home-automation/internal/myip"
)

func main() {
	ip, err := myip.SeeIp(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get public IP: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("IPv4: %v", ip)
}
