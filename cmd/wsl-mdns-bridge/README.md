# WSL mDNS Bridge

A Windows service that bridges mDNS (multicast DNS) traffic between the physical network interface and the WSL virtual network interface, enabling mDNS service discovery to work seamlessly in WSL environments.

## Problem

By default, WSL networking is isolated from the Windows host's physical network. This means:
- mDNS queries from WSL don't reach the physical network
- mDNS responses from the physical network don't reach WSL
- Service discovery (Avahi, Bonjour, etc.) doesn't work in WSL

## Solution

This bridge listens for mDNS multicast packets on both interfaces and forwards them bidirectionally, making WSL behave like a native Linux host on the physical network for mDNS purposes.

## How It Works

### Architecture

1. **Single Socket per IP Version**: Creates one UDP socket per IP version (IPv4 and IPv6) that binds to `0.0.0.0:5353` (or `[::]:5353`)
2. **Multicast Group Membership**: Joins the mDNS multicast group (`224.0.0.251` for IPv4, `ff02::fb` for IPv6) on both the WSL and host interfaces
3. **Interface-Aware Forwarding**: Uses control messages (`FlagDst` and `FlagInterface`) to determine which interface received each packet and forwards it to the other interface's multicast group
4. **Proper Multicast Addressing**: Sends forwarded packets to the multicast group address with the correct outgoing interface specified via control messages

### Key Technical Details

- **Correct Socket Binding**: Binds to `0.0.0.0:5353` (not the multicast address) to receive multicast packets
- **SO_REUSEADDR**: Enables port sharing with other mDNS responders on the system
- **Control Messages**: Enables `FlagDst` and `FlagInterface` to receive packet metadata and set outgoing interface
- **Multicast Loopback**: **Enabled** to allow the bridge to receive packets forwarded to the multicast group
- **Bidirectional Forwarding**: Single goroutine per IP version reads packets, determines source interface, and forwards to the other interface only

## Usage

### Basic Usage (Auto-detect interfaces)

```powershell
# Run as normal user (no admin privileges required)
.\wsl-mdns-bridge.exe
```

The bridge will automatically:
- Detect the WSL interface (looks for "wsl" or "vethernet" in the name)
- Detect the host interface (finds the interface with the default gateway route)

### Manual Interface Selection

```powershell
# Specify interfaces explicitly
.\wsl-mdns-bridge.exe -wsl "vEthernet (WSL)" -host "Ethernet"
```

### List Available Interfaces

```powershell
# PowerShell
Get-NetAdapter | Select-Object Name, InterfaceDescription, Status

# Command Prompt
netsh interface ipv4 show interfaces
```

## Requirements

- **No Administrator Privileges Required**: The bridge uses `SO_REUSEADDR` to share port 5353 with other processes (like Edge browser, TeamViewer, etc.)
- **Windows Firewall**: The bridge may need firewall rules to allow UDP port 5353 traffic
  - Inbound rule for UDP 5353 on multicast addresses
  - Outbound rule for UDP 5353 on multicast addresses

### Port 5353 Sharing

The bridge automatically sets the `SO_REUSEADDR` socket option, allowing it to coexist with other mDNS responders on the system, such as:
- Microsoft Edge browser
- EdgeWebView2
- Windows mDNS service (svchost)
- TeamViewer
- Bonjour/iTunes
- Other mDNS clients

### Windows Firewall Rules

If the bridge doesn't work, you may need to add firewall rules:

```powershell
# Allow inbound mDNS (IPv4)
New-NetFirewallRule -DisplayName "mDNS Bridge IPv4 Inbound" `
    -Direction Inbound -Protocol UDP -LocalPort 5353 `
    -RemoteAddress 224.0.0.0/4 -Action Allow

# Allow outbound mDNS (IPv4)
New-NetFirewallRule -DisplayName "mDNS Bridge IPv4 Outbound" `
    -Direction Outbound -Protocol UDP -LocalPort 5353 `
    -RemoteAddress 224.0.0.0/4 -Action Allow

# Allow inbound mDNS (IPv6)
New-NetFirewallRule -DisplayName "mDNS Bridge IPv6 Inbound" `
    -Direction Inbound -Protocol UDP -LocalPort 5353 `
    -RemoteAddress ff00::/8 -Action Allow

# Allow outbound mDNS (IPv6)
New-NetFirewallRule -DisplayName "mDNS Bridge IPv6 Outbound" `
    -Direction Outbound -Protocol UDP -LocalPort 5353 `
    -RemoteAddress ff00::/8 -Action Allow
```

## Testing

### From WSL

```bash
# Install avahi-utils
sudo apt-get install avahi-utils

# Browse for services
avahi-browse -a

# Resolve a hostname
avahi-resolve -n hostname.local
```

### From Windows

```powershell
# Use dns-sd (part of Bonjour)
dns-sd -B _http._tcp

# Or use a tool like Discovery - DNS-SD Browser
```

## Logging

The bridge logs all forwarded packets with details:
- Packet size
- Source interface
- Destination interface
- Source address

Example output:
```
2025/10/06 21:25:00 Auto-detected WSL interface: vEthernet (WSL)
2025/10/06 21:25:00 Auto-detected host interface (default route): Ethernet
2025/10/06 21:25:00 Setting up mDNS bridge between vEthernet (WSL) and Ethernet
2025/10/06 21:25:00 Joined IPv4 multicast group on vEthernet (WSL)
2025/10/06 21:25:00 Joined IPv4 multicast group on Ethernet
2025/10/06 21:25:00 IPv4 multicast bridge ready
2025/10/06 21:25:00 Started IPv4 packet forwarding
2025/10/06 21:25:00 mDNS bridge is running
2025/10/06 21:25:05 Forwarded 45 bytes IPv4: vEthernet (WSL) -> Ethernet (from 172.20.0.2:5353)
```

## Running as a Service

To run the bridge automatically on Windows startup, you can:

1. **Use Task Scheduler** (recommended):
   - Create a new task
   - Trigger: At system startup
   - Action: Start program `wsl-mdns-bridge.exe`
   - Run with highest privileges

2. **Use NSSM** (Non-Sucking Service Manager):
   ```powershell
   nssm install WSL-mDNS-Bridge "C:\path\to\wsl-mdns-bridge.exe"
   nssm start WSL-mDNS-Bridge
   ```

## Troubleshooting

### Bridge doesn't start

- **Check interfaces**: Verify the detected interfaces are correct
  ```powershell
  # List all network interfaces
  Get-NetAdapter | Select-Object Name, InterfaceDescription, Status
  ```
- **Port 5353 already in use**: The bridge uses `SO_REUSEADDR` to share the port, but if you still get bind errors, check what's using it:
  ```powershell
  netstat -ano | findstr :5353
  Get-Process -Id <PID> | Select-Object Id,ProcessName,Path
  ```

### No packets forwarded

- **Check Windows Firewall**: Add the firewall rules mentioned above
- **Check multicast routing**: Ensure multicast is enabled on both interfaces
  ```powershell
  Get-NetIPInterface | Select-Object InterfaceAlias, AddressFamily, MulticastForwarding
  ```
- **Check WSL networking mode**: The bridge works best with NAT mode (default)

### IPv6 not working

- IPv6 support is optional and may not work on all systems
- The bridge will continue with IPv4-only if IPv6 setup fails
- Check if IPv6 is enabled on both interfaces

## Technical Notes

### Implementation Details

The bridge uses the following approach:

1. **Single socket per IP version**: One socket joins multicast groups on both interfaces
2. **Control messages**: `SetControlMessage(FlagDst|FlagInterface, true)` enables interface detection
3. **Interface-based routing**: Reads `cm.IfIndex` to determine source, sets it to forward to the other interface
4. **Multicast loopback enabled**: Required to receive packets sent to the multicast group by the bridge itself
5. **Selective forwarding**: Only forwards between the two configured interfaces (WSL ↔ Host), ignoring packets from other interfaces

### mDNS Protocol

- **Port**: UDP 5353
- **IPv4 Multicast**: 224.0.0.251
- **IPv6 Multicast**: ff02::fb (link-local)
- **TTL**: Typically 255 for queries, 255 for responses

## Building

```powershell
go build -o wsl-mdns-bridge.exe .
```

## License

Same as the parent project.
