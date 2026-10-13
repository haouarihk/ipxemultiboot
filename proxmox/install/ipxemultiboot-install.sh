#!/usr/bin/env bash

# Copyright (c) 2021-2026 community-scripts ORG
# Author: iPXE Multiboot contributors
# License: MIT | https://github.com/community-scripts/DevScripts/raw/main/LICENSE
# Source: https://github.com/haouarihk/ipxemultiboot

source /dev/stdin <<<"$FUNCTIONS_FILE_PATH"
color
verb_ip6
catch_errors
setting_up_container
network_check
update_os

fetch_and_deploy_gh_release "ipxemultiboot" "haouarihk/ipxemultiboot" "prebuild" "latest" "/opt/ipxemultiboot" "ipxemultiboot_*_linux_$(arch_resolve "amd64" "arm64").tar.gz"

msg_info "Configuring iPXE Multiboot"
mkdir -p /opt/ipxemultiboot_data/isos
cat <<EOF >/opt/ipxemultiboot_data/ipxemultiboot.env
# Defaults only: settings saved in the web interface take precedence.
IPXEMULTIBOOT_DATA_DIR=/opt/ipxemultiboot_data
IPXEMULTIBOOT_ISO_DIR=/opt/ipxemultiboot_data/isos
IPXEMULTIBOOT_DHCP_MODE=proxy
IPXEMULTIBOOT_HTTP_PORT=8080
EOF
msg_ok "Configured iPXE Multiboot"

msg_info "Creating Service"
cat <<EOF >/etc/systemd/system/ipxemultiboot.service
[Unit]
Description=iPXE Multiboot network boot server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/opt/ipxemultiboot_data/ipxemultiboot.env
ExecStart=/opt/haouarihk/ipxemultiboot
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
systemctl enable -q --now ipxemultiboot
msg_ok "Created Service"

motd_ssh
customize
cleanup_lxc
