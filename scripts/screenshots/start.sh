#!/bin/sh
# Starts a few well-known services, then proctab in the foreground.
case "$1" in
  web)   exec nginx -g 'daemon off;' ;;
  cache) exec redis-server --protected-mode no ;;
  api)   cd /srv/app && exec su app -c 'python3 -m http.server 8000' ;;
  old)   exit 3 ;;
esac
dropbear -R -p 22
dnsmasq --no-resolv --port=53
nginx
redis-server --daemonize yes --protected-mode no
memcached -u root -p 0 -U 0 -d          # running, but no port (yellow)
su app -c 'cd /srv/app && python3 -m http.server 8000 >/dev/null 2>&1 &'
su app -c 'python3 /srv/app/worker.py 0.45 &'
python3 /srv/app/worker.py 0.15 &
sleep 2
exec proctab "$@"
