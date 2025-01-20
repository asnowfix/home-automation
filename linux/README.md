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