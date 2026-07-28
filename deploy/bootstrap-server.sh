#!/bin/sh
set -eu

deploy_public_key="${1:-}"
if test -z "$deploy_public_key"; then
	echo "usage: bootstrap-server.sh 'ssh-ed25519 AAAA... deploy-key'" >&2
	exit 2
fi

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends \
	apt-transport-https \
	ca-certificates \
	curl \
	debian-keyring \
	debian-archive-keyring \
	gnupg \
	ufw

install -d -m 0755 /usr/share/keyrings
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key |
	gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt \
	-o /etc/apt/sources.list.d/caddy-stable.list
chmod 0644 \
	/usr/share/keyrings/caddy-stable-archive-keyring.gpg \
	/etc/apt/sources.list.d/caddy-stable.list

apt-get update
apt-get install -y --no-install-recommends caddy

if ! getent passwd khapi >/dev/null; then
	useradd --system \
		--home-dir /var/lib/khmer-holiday-api \
		--shell /usr/sbin/nologin \
		--user-group khapi
fi
if ! getent passwd deployer >/dev/null; then
	useradd --create-home --shell /bin/bash --user-group deployer
fi

install -d -m 0750 -o khapi -g khapi /var/lib/khmer-holiday-api
install -d -m 0755 /opt/khmer-holiday-api/bin /opt/khmer-holiday-api/releases

install -d -m 0700 -o deployer -g deployer /home/deployer/.ssh
printf '%s\n' "$deploy_public_key" > /home/deployer/.ssh/authorized_keys
chown deployer:deployer /home/deployer/.ssh/authorized_keys
chmod 0600 /home/deployer/.ssh/authorized_keys

install -m 0755 deploy/remote-deploy.sh /usr/local/sbin/deploy-khapi
printf '%s\n' \
	'deployer ALL=(root) NOPASSWD: /usr/local/sbin/deploy-khapi /tmp/khapi-release-*.tar.gz' \
	> /etc/sudoers.d/khapi-deploy
chmod 0440 /etc/sudoers.d/khapi-deploy
visudo -cf /etc/sudoers.d/khapi-deploy

install -d -m 0755 /etc/ssh/sshd_config.d
printf '%s\n' \
	'PasswordAuthentication no' \
	'KbdInteractiveAuthentication no' \
	'PermitRootLogin prohibit-password' \
	> /etc/ssh/sshd_config.d/99-khapi-hardening.conf
sshd -t
systemctl reload ssh

ufw default deny incoming
ufw default allow outgoing
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

systemctl disable --now caddy.service

echo "server bootstrap complete"
