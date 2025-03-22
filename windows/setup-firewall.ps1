#Requires -RunAsAdministrator

# Script to set up Windows Firewall rules for MyHome Automation
# Must be run with administrator privileges

$rules = @(
    @{
        Name = "MyHome mDNS IPv4"
        Description = "MyHome Automation mDNS IPv4 Multicast"
        Protocol = "UDP"
        LocalPort = 5353
        RemoteAddress = "224.0.0.0/4"
    },
    @{
        Name = "MyHome mDNS IPv6"
        Description = "MyHome Automation mDNS IPv6 Multicast"
        Protocol = "UDP"
        LocalPort = 5353
        RemoteAddress = "ff00::/8"
    }
)

# Function to remove rule if it exists
function Remove-FirewallRule {
    param($Name)
    $existing = Get-NetFirewallRule -Name $Name -ErrorAction SilentlyContinue
    if ($existing) {
        Write-Host "Removing existing rule: $Name"
        Remove-NetFirewallRule -Name $Name
    }
}

# Remove existing rules first
foreach ($rule in $rules) {
    Remove-FirewallRule -Name $rule.Name
}

# Create new rules
foreach ($rule in $rules) {
    Write-Host "Creating rule: $($rule.Name)"
    New-NetFirewallRule `
        -Name $rule.Name `
        -DisplayName $rule.Name `
        -Description $rule.Description `
        -Direction Inbound `
        -Protocol $rule.Protocol `
        -LocalPort $rule.LocalPort `
        -RemoteAddress $rule.RemoteAddress `
        -Action Allow `
        -Enabled True
}

Write-Host "Firewall rules setup complete!"