# Setup on Linux

## Pi-OS -- Raspbian

[x] Install basic image with custom settings

    - hostname: myhome.local


[x] Enable sshd

    - sudo raspi-config

    Or:

    - sudo apt-get update
    - sudo apt-get install openssh-server
    - sudo systemctl enable ssh
    - sudo systemctl start ssh
    - sudo systemctl status ssh

[x] Enable ssh login

    ```bash
    ssh-copy-id -i ~/.ssh/id_rsa.pub admin@myhome.local
    ssh -i ~/.ssh/id_rsa admin@myhome.local
    ```

    ```log
    Linux myhome 6.6.51+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.6.51-1+rpt3 (2024-10-08) aarch64
    ```

    ```config
    Host myhome
      HostName myhome.local.
      User admin
      IdentityFile ~/.ssh/id_rsa
    End

[ ] Setup passwordless sudo
[ ] Setup mDNS
[ ] Setup systemd service
[ ] Disable LXD graphical UI (login from console or ssh only)

## Directory Structure

```
linux/
├── debian/           # Debian package scripts
│   ├── postinst.sh   # Post-installation (enable & start services)
│   ├── prerm.sh      # Pre-removal (stop services)
│   └── postrm.sh     # Post-removal (disable services)
├── systemd/          # Systemd units and helper scripts
│   ├── myhome.service
│   ├── myhome-update.service
│   ├── myhome-update.timer
│   ├── myhome-db-backup.service
│   ├── myhome-db-backup.timer
│   ├── myhome-db-backup.sh
│   └── update.sh
└── README.md
```

## Systemd Services

### myhome.service
Main MyHome daemon service.

```bash
sudo systemctl enable myhome
sudo systemctl start myhome
```

### myhome-update.timer
Daily automatic update from GitHub releases.

```bash
sudo systemctl enable myhome-update.timer
sudo systemctl start myhome-update.timer
```

### myhome-db-backup.timer
Daily database backup to `/var/lib/myhome/backups/`.

```bash
sudo systemctl enable myhome-db-backup.timer
sudo systemctl start myhome-db-backup.timer
```

Backups are stored as timestamped JSON files with automatic rotation (keeps last 30 backups).
A symlink `devices-latest.json` always points to the most recent backup.

To manually trigger a backup:
```bash
sudo systemctl start myhome-db-backup.service
```