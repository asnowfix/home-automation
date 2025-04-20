package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/jackpal/gateway"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	mDNSPort     = 5353
	bufferSize   = 65535
	mDNSAddrIPv4 = "224.0.0.251"
	mDNSAddrIPv6 = "ff02::fb"
)

type Bridge struct {
	wslInterface  *net.Interface
	hostInterface *net.Interface
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func findWSLInterface() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to list network interfaces: %v", err)
	}

	for _, iface := range interfaces {
		name := strings.ToLower(iface.Name)
		// WSL interfaces typically have "wsl" in their name
		if strings.Contains(name, "wsl") || strings.Contains(name, "vethernet") {
			return iface.Name, nil
		}
	}

	return "", fmt.Errorf("no WSL interface found")
}

func findDefaultRouteInterface() (string, error) {
	// Get the default gateway IP
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return "", fmt.Errorf("failed to discover gateway: %v", err)
	}

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to list network interfaces: %v", err)
	}

	// Find the interface that has the route to the gateway
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			// Get IP network from address
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Check if the gateway IP is in this network
			if ipNet.Contains(gatewayIP) {
				return iface.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no interface found with route to gateway %v", gatewayIP)
}

func NewBridge(wslInterfaceName, hostInterfaceName string) (*Bridge, error) {
	wslIface, err := net.InterfaceByName(wslInterfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find WSL interface %s: %v", wslInterfaceName, err)
	}

	hostIface, err := net.InterfaceByName(hostInterfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find host interface %s: %v", hostInterfaceName, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Bridge{
		wslInterface:  wslIface,
		hostInterface: hostIface,
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

func (b *Bridge) listenMulticast(iface *net.Interface, isIPv6 bool) (*net.UDPConn, error) {
	var addr *net.UDPAddr
	if isIPv6 {
		addr = &net.UDPAddr{
			IP:   net.ParseIP(mDNSAddrIPv6),
			Port: mDNSPort,
		}
	} else {
		addr = &net.UDPAddr{
			IP:   net.ParseIP(mDNSAddrIPv4),
			Port: mDNSPort,
		}
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	if err := conn.SetReadBuffer(bufferSize); err != nil {
		conn.Close()
		return nil, err
	}

	if isIPv6 {
		p := ipv6.NewPacketConn(conn)
		if err := p.JoinGroup(iface, &net.UDPAddr{IP: addr.IP}); err != nil {
			conn.Close()
			return nil, err
		}
	} else {
		p := ipv4.NewPacketConn(conn)
		if err := p.JoinGroup(iface, &net.UDPAddr{IP: addr.IP}); err != nil {
			conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (b *Bridge) forwardPackets(src *net.UDPConn, dst *net.UDPConn, name string) {
	defer b.wg.Done()
	buffer := make([]byte, bufferSize)

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			n, srcAddr, err := src.ReadFromUDP(buffer)
			if err != nil {
				if b.ctx.Err() != nil && strings.Contains(err.Error(), "use of closed network connection") {
					// Graceful shutdown, don't log as error
					return
				}
				log.Printf("Error reading from %s: %v", name, err)
				continue
			}

			_, err = dst.Write(buffer[:n])
			if err != nil {
				log.Printf("Error forwarding from %s: %v", name, err)
				continue
			}

			log.Printf("Forwarded %d bytes from %s (%s)", n, name, srcAddr)
		}
	}
}

func (b *Bridge) Start() error {
	// Set up IPv4 connections
	wslConnv4, err := b.listenMulticast(b.wslInterface, false)
	if err != nil {
		return fmt.Errorf("failed to set up WSL IPv4 listener: %v", err)
	}
	defer wslConnv4.Close()

	hostConnv4, err := b.listenMulticast(b.hostInterface, false)
	if err != nil {
		return fmt.Errorf("failed to set up host IPv4 listener: %v", err)
	}
	defer hostConnv4.Close()

	// Set up IPv6 connections
	wslConnv6, err := b.listenMulticast(b.wslInterface, true)
	if err != nil {
		log.Printf("Warning: failed to set up WSL IPv6 listener: %v", err)
	} else {
		defer wslConnv6.Close()
	}

	hostConnv6, err := b.listenMulticast(b.hostInterface, true)
	if err != nil {
		log.Printf("Warning: failed to set up host IPv6 listener: %v", err)
	} else {
		defer hostConnv6.Close()
	}

	// Start forwarding routines
	b.wg.Add(2)
	go b.forwardPackets(wslConnv4, hostConnv4, "WSL->Host IPv4")
	go b.forwardPackets(hostConnv4, wslConnv4, "Host->WSL IPv4")

	if wslConnv6 != nil && hostConnv6 != nil {
		b.wg.Add(2)
		go b.forwardPackets(wslConnv6, hostConnv6, "WSL->Host IPv6")
		go b.forwardPackets(hostConnv6, wslConnv6, "Host->WSL IPv6")
	}

	return nil
}

func (b *Bridge) Stop() {
	b.cancel()
	b.wg.Wait()
}

func main() {
	wslIface := flag.String("wsl", "", "WSL interface name (optional, will auto-detect if not provided)")
	hostIface := flag.String("host", "", "Host interface name (optional, will use default route interface if not provided)")
	flag.Parse()

	// Auto-detect WSL interface if not provided
	wslIfaceName := *wslIface
	if wslIfaceName == "" {
		var err error
		wslIfaceName, err = findWSLInterface()
		if err != nil {
			log.Fatalf("Failed to auto-detect WSL interface: %v", err)
		}
		log.Printf("Auto-detected WSL interface: %s", wslIfaceName)
	}

	// Auto-detect host interface if not provided
	hostIfaceName := *hostIface
	if hostIfaceName == "" {
		var err error
		hostIfaceName, err = findDefaultRouteInterface()
		if err != nil {
			log.Fatalf("Failed to auto-detect host interface: %v", err)
		}
		log.Printf("Auto-detected host interface (default route): %s", hostIfaceName)
	}

	bridge, err := NewBridge(wslIfaceName, hostIfaceName)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}

	<-sigChan
	bridge.Stop()
}
