#!/bin/sh
set -e

# Подставляем переменные окружения в HTML
envsubst '$SERVICE_NAME $SERVICE_TAGLINE $CABINET_URL' < /usr/share/nginx/html/index.html > /tmp/index.html
mv /tmp/index.html /usr/share/nginx/html/index.html

exec nginx -g 'daemon off;'
