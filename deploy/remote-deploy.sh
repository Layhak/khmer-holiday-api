#!/bin/sh
set -eu

archive="${1:-}"
case "$archive" in
	/tmp/khapi-release-*.tar.gz) ;;
	*)
		echo "invalid release archive path: $archive" >&2
		exit 2
		;;
esac

checksum="${archive}.sha256"
test -f "$archive"
test -f "$checksum"

cd /tmp
sha256sum -c "$checksum"

stage="$(mktemp -d /tmp/khapi-deploy.XXXXXX)"
backup=""
cleanup() {
	rm -rf "$stage"
	rm -f "$archive" "$checksum"
}
trap cleanup EXIT INT TERM

tar -xzf "$archive" -C "$stage" --no-same-owner
release_dir="$(find "$stage" -mindepth 1 -maxdepth 1 -type d -name 'release-*' -print -quit)"
test -n "$release_dir"

for required in \
	bin/khapi \
	bin/khapi-scrape \
	deploy/Caddyfile \
	deploy/khapi.service \
	deploy/khapi-scrape.service \
	deploy/khapi-scrape.timer
do
	test -f "$release_dir/$required"
done

caddy validate --config "$release_dir/deploy/Caddyfile"

install -d -m 0755 /opt/khmer-holiday-api/bin /opt/khmer-holiday-api/releases
install -d -m 0750 -o khapi -g khapi /var/lib/khmer-holiday-api

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
if test -x /opt/khmer-holiday-api/bin/khapi; then
	backup="/opt/khmer-holiday-api/releases/$stamp"
	install -d -m 0755 "$backup/bin"
	cp -p /opt/khmer-holiday-api/bin/khapi "$backup/bin/khapi"
	if test -x /opt/khmer-holiday-api/bin/khapi-scrape; then
		cp -p /opt/khmer-holiday-api/bin/khapi-scrape "$backup/bin/khapi-scrape"
	fi
fi

install -m 0755 "$release_dir/bin/khapi" /opt/khmer-holiday-api/bin/khapi
install -m 0755 "$release_dir/bin/khapi-scrape" /opt/khmer-holiday-api/bin/khapi-scrape
install -m 0644 "$release_dir/deploy/Caddyfile" /etc/caddy/Caddyfile
install -m 0644 "$release_dir/deploy/khapi.service" /etc/systemd/system/khapi.service
install -m 0644 "$release_dir/deploy/khapi-scrape.service" /etc/systemd/system/khapi-scrape.service
install -m 0644 "$release_dir/deploy/khapi-scrape.timer" /etc/systemd/system/khapi-scrape.timer

systemctl daemon-reload
systemctl enable khapi.service khapi-scrape.timer

if test ! -s /var/lib/khmer-holiday-api/holidays.db; then
	current_year="$(date +%Y)"
	first_year=$((current_year - 1))
	last_year=$((current_year + 1))
	runuser -u khapi -- \
		/opt/khmer-holiday-api/bin/khapi-scrape scrape \
		-years "$first_year-$last_year" \
		-db /var/lib/khmer-holiday-api/holidays.db
fi

systemctl restart khapi.service
systemctl enable --now khapi-scrape.timer

if systemctl is-active --quiet caddy.service; then
	systemctl reload caddy.service
else
	systemctl enable --now caddy.service
fi

healthy=false
attempt=0
while test "$attempt" -lt 12; do
	if curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null; then
		healthy=true
		break
	fi
	attempt=$((attempt + 1))
	sleep 2
done

if test "$healthy" != true; then
	echo "new API release failed its health check" >&2
	journalctl -u khapi.service --no-pager -n 50 >&2 || true
	if test -n "$backup" && test -x "$backup/bin/khapi"; then
		echo "rolling back to $backup" >&2
		install -m 0755 "$backup/bin/khapi" /opt/khmer-holiday-api/bin/khapi
		if test -x "$backup/bin/khapi-scrape"; then
			install -m 0755 "$backup/bin/khapi-scrape" /opt/khmer-holiday-api/bin/khapi-scrape
		fi
		systemctl restart khapi.service
	fi
	exit 1
fi

find /opt/khmer-holiday-api/releases \
	-mindepth 1 -maxdepth 1 -type d \
	-mtime +30 -exec rm -rf {} +

echo "deployment complete: $stamp"
