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
	
	// IPv4 connections - separate socket per interface
	wslIPv4Conn  *ipv4.PacketConn
	wslIPv4UDP   *net.UDPConn
	hostIPv4Conn *ipv4.PacketConn
	hostIPv4UDP  *net.UDPConn
	
	// IPv6 connections - separate socket per interface
	wslIPv6Conn  *ipv6.PacketConn
	wslIPv6UDP   *net.UDPConn
	hostIPv6Conn *ipv6.PacketConn
	hostIPv6UDP  *net.UDPConn
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

func (b *Bridge) setupIPv4Socket(iface *net.Interface) (*ipv4.PacketConn, *net.UDPConn, error) {
	// Create a ListenConfig with SO_REUSEADDR to allow multiple processes to bind to port 5353
	lc := &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var sockOptErr error
			err := c.Control(func(fd uintptr) {
				// Set SO_REUSEADDR to allow multiple bindings to the same port
				sockOptErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
			if err != nil {
				return err
			}
			return sockOptErr
		},
	}
	
	// Bind to 0.0.0.0:5353 to receive multicast packets
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: mDNSPort,
	}
	
	// Listen with the configured socket options
	packetConn, err := lc.ListenPacket(context.Background(), "udp4", addr.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create IPv4 socket: %v", err)
	}
	
	conn := packetConn.(*net.UDPConn)
	
	// Set socket options
	if err := conn.SetReadBuffer(bufferSize); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to set read buffer: %v", err)
	}
	
	// Create packet conn for multicast control
	p := ipv4.NewPacketConn(conn)
	
	// Join multicast group on this interface
	mcastAddr := &net.UDPAddr{IP: net.ParseIP(mDNSAddrIPv4)}
	
	if err := p.JoinGroup(iface, mcastAddr); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to join multicast group: %v", err)
	}
	
	// Enable multicast loopback - CRITICAL for bridge to work
	if err := p.SetMulticastLoopback(true); err != nil {
		log.Printf("Warning: failed to enable multicast loopback: %v", err)
	}
	
	// Set multicast TTL
	if err := p.SetMulticastTTL(255); err != nil {
		log.Printf("Warning: failed to set multicast TTL: %v", err)
	}
	
	return p, conn, nil
}

func (b *Bridge) setupIPv4() error {
	// Create socket for WSL interface
	wslConn, wslUDP, err := b.setupIPv4Socket(b.wslInterface)
	if err != nil {
		return fmt.Errorf("failed to set up WSL IPv4 socket: %v", err)
	}
	b.wslIPv4Conn = wslConn
	b.wslIPv4UDP = wslUDP
	log.Printf("Joined IPv4 multicast group on %s", b.wslInterface.Name)
	
	// Create socket for host interface
	hostConn, hostUDP, err := b.setupIPv4Socket(b.hostInterface)
	if err != nil {
		wslUDP.Close()
		return fmt.Errorf("failed to set up host IPv4 socket: %v", err)
	}
	b.hostIPv4Conn = hostConn
	b.hostIPv4UDP = hostUDP
	log.Printf("Joined IPv4 multicast group on %s", b.hostInterface.Name)
	
	return nil
}

func (b *Bridge) setupIPv6Socket(iface *net.Interface) (*ipv6.PacketConn, *net.UDPConn, error) {
	// Create a ListenConfig with SO_REUSEADDR to allow multiple processes to bind to port 5353
	lc := &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var sockOptErr error
			err := c.Control(func(fd uintptr) {
				// Set SO_REUSEADDR to allow multiple bindings to the same port
				sockOptErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
			if err != nil {
				return err
			}
			return sockOptErr
		},
	}
	
	// Bind to [::]:5353 to receive multicast packets
	addr := &net.UDPAddr{
		IP:   net.IPv6zero,
		Port: mDNSPort,
	}
	
	// Listen with the configured socket options
	packetConn, err := lc.ListenPacket(context.Background(), "udp6", addr.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create IPv6 socket: %v", err)
	}
	
	conn := packetConn.(*net.UDPConn)
	
	// Set socket options
	if err := conn.SetReadBuffer(bufferSize); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to set read buffer: %v", err)
	}
	
	// Create packet conn for multicast control
	p := ipv6.NewPacketConn(conn)
	
	// Join multicast group on this interface
	mcastAddr := &net.UDPAddr{IP: net.ParseIP(mDNSAddrIPv6)}
	
	if err := p.JoinGroup(iface, mcastAddr); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to join multicast group: %v", err)
	}
	
	// Enable multicast loopback - CRITICAL for bridge to work
	if err := p.SetMulticastLoopback(true); err != nil {
		log.Printf("Warning: failed to enable multicast loopback: %v", err)
	}
	
	// Set multicast hop limit
	if err := p.SetMulticastHopLimit(255); err != nil {
		log.Printf("Warning: failed to set multicast hop limit: %v", err)
	}
	
	return p, conn, nil
}

func (b *Bridge) setupIPv6() error {
	// Create socket for WSL interface
	wslConn, wslUDP, err := b.setupIPv6Socket(b.wslInterface)
	if err != nil {
		return fmt.Errorf("failed to set up WSL IPv6 socket: %v", err)
	}
	b.wslIPv6Conn = wslConn
	b.wslIPv6UDP = wslUDP
	log.Printf("Joined IPv6 multicast group on %s", b.wslInterface.Name)
	
	// Create socket for host interface
	hostConn, hostUDP, err := b.setupIPv6Socket(b.hostInterface)
	if err != nil {
		wslUDP.Close()
		return fmt.Errorf("failed to set up host IPv6 socket: %v", err)
	}
	b.hostIPv6Conn = hostConn
	b.hostIPv6UDP = hostUDP
	log.Printf("Joined IPv6 multicast group on %s", b.hostInterface.Name)
	
	return nil
}

func (b *Bridge) forwardIPv4Packets(srcConn *ipv4.PacketConn, dstConn *ipv4.PacketConn, srcName, dstName string) {
	defer b.wg.Done()
	buffer := make([]byte, bufferSize)
	mcastAddr := &net.UDPAddr{
		IP:   net.ParseIP(mDNSAddrIPv4),
		Port: mDNSPort,
	}

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Read packet from source interface
			n, _, srcAddr, err := srcConn.ReadFrom(buffer)
			if err != nil {
				if b.ctx.Err() != nil {
					return
				}
				log.Printf("Error reading IPv4 packet from %s: %v", srcName, err)
				continue
			}

			// Forward to destination interface's multicast group
			_, err = dstConn.WriteTo(buffer[:n], nil, mcastAddr)
			if err != nil {
				log.Printf("Error forwarding IPv4 packet from %s to %s: %v", 
					srcName, dstName, err)
				continue
			}

			log.Printf("Forwarded %d bytes IPv4: %s -> %s (from %s)", 
				n, srcName, dstName, srcAddr)
		}
	}
}

func (b *Bridge) forwardIPv6Packets(srcConn *ipv6.PacketConn, dstConn *ipv6.PacketConn, srcName, dstName string) {
	defer b.wg.Done()
	buffer := make([]byte, bufferSize)
	mcastAddr := &net.UDPAddr{
		IP:   net.ParseIP(mDNSAddrIPv6),
		Port: mDNSPort,
	}

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Read packet from source interface
			n, _, srcAddr, err := srcConn.ReadFrom(buffer)
			if err != nil {
				if b.ctx.Err() != nil {
					return
				}
				log.Printf("Error reading IPv6 packet from %s: %v", srcName, err)
				continue
			}

			// Forward to destination interface's multicast group
			_, err = dstConn.WriteTo(buffer[:n], nil, mcastAddr)
			if err != nil {
				log.Printf("Error forwarding IPv6 packet from %s to %s: %v", 
					srcName, dstName, err)
				continue
			}

			log.Printf("Forwarded %d bytes IPv6: %s -> %s (from %s)", 
				n, srcName, dstName, srcAddr)
		}
	}
}

func (b *Bridge) Start() error {
	log.Printf("Setting up mDNS bridge between %s and %s", 
		b.wslInterface.Name, b.hostInterface.Name)
	
	// Set up IPv4
	if err := b.setupIPv4(); err != nil {
		return fmt.Errorf("failed to set up IPv4: %v", err)
	}
	log.Printf("IPv4 multicast bridge ready")

	// Set up IPv6 (optional, may fail on some systems)
	if err := b.setupIPv6(); err != nil {
		log.Printf("Warning: IPv6 setup failed (continuing with IPv4 only): %v", err)
	} else {
		log.Printf("IPv6 multicast bridge ready")
	}

	// Start forwarding goroutines for IPv4 (bidirectional)
	b.wg.Add(2)
	go b.forwardIPv4Packets(b.wslIPv4Conn, b.hostIPv4Conn, b.wslInterface.Name, b.hostInterface.Name)
	go b.forwardIPv4Packets(b.hostIPv4Conn, b.wslIPv4Conn, b.hostInterface.Name, b.wslInterface.Name)
	log.Printf("Started IPv4 packet forwarding")

	// Start forwarding goroutines for IPv6 (bidirectional) if available
	if b.wslIPv6Conn != nil && b.hostIPv6Conn != nil {
		b.wg.Add(2)
		go b.forwardIPv6Packets(b.wslIPv6Conn, b.hostIPv6Conn, b.wslInterface.Name, b.hostInterface.Name)
		go b.forwardIPv6Packets(b.hostIPv6Conn, b.wslIPv6Conn, b.hostInterface.Name, b.wslInterface.Name)
		log.Printf("Started IPv6 packet forwarding")
	}

	log.Printf("mDNS bridge is running")
	return nil
}

func (b *Bridge) Stop() {
	log.Printf("Stopping mDNS bridge...")
	b.cancel()
	
	// Close connections to unblock ReadFrom calls
	if b.wslIPv4UDP != nil {
		b.wslIPv4UDP.Close()
	}
	if b.hostIPv4UDP != nil {
		b.hostIPv4UDP.Close()
	}
	if b.wslIPv6UDP != nil {
		b.wslIPv6UDP.Close()
	}
	if b.hostIPv6UDP != nil {
		b.hostIPv6UDP.Close()
	}
	
	b.wg.Wait()
	log.Printf("mDNS bridge stopped")
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
