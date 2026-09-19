#!/bin/sh
set -e

# Extract hostname from BACKEND_URL for the Host header
BACKEND_HOST=$(echo "$BACKEND_URL" | sed -e 's|https\?://||' -e 's|/.*||' -e 's|:.*||')
export BACKEND_HOST

# Pull the first nameserver from /etc/resolv.conf so nginx's `resolver`
# directive uses whatever DNS the container is actually configured with —
# Docker Compose sets 127.0.0.11, Railway sets its own internal resolver
# that knows about *.railway.internal. Hardcoding 8.8.8.8 (the previous
# behavior) breaks Railway's private network because public DNS can't see
# Railway's internal namespace. Falls back to 8.8.8.8 if /etc/resolv.conf
# is unreadable so the build still works on minimal images.
NGINX_RESOLVER=$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf 2>/dev/null || true)
NGINX_RESOLVER=${NGINX_RESOLVER:-8.8.8.8}
# Wrap raw IPv6 addresses in [] for nginx; IPv4 stays bare.
case "$NGINX_RESOLVER" in
    *:*) NGINX_RESOLVER="[$NGINX_RESOLVER]" ;;
esac
export NGINX_RESOLVER

echo "Substituting environment variables..."
echo "PORT=$PORT"
echo "BACKEND_URL=$BACKEND_URL"
echo "BACKEND_HOST=$BACKEND_HOST"
echo "NGINX_RESOLVER=$NGINX_RESOLVER"

envsubst '$PORT $BACKEND_URL $BACKEND_HOST $NGINX_RESOLVER' < /etc/nginx/nginx.conf.template > /etc/nginx/nginx.conf

echo "Generated nginx config:"
cat /etc/nginx/nginx.conf | grep -A6 "location /api" || true

echo "Starting nginx..."
exec nginx -g 'daemon off;'
