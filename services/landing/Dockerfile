FROM nginx:alpine

COPY services/landing/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY services/landing/pages/index.html /usr/share/nginx/html/index.html
COPY services/landing/pages/assets /usr/share/nginx/html/assets
COPY services/landing/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
