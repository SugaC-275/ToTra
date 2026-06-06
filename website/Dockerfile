FROM nginx:1.27-alpine
RUN apk add --no-cache gettext
COPY index.html style.css interactions.css interactions.js content-styles.css scene.js /usr/share/nginx/html/
COPY assets/ /usr/share/nginx/html/assets/
COPY nginx.conf /etc/nginx/nginx.conf.template
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:${PORT:-80}/
CMD ["/bin/sh", "-c", "export PORT=${PORT:-80} && envsubst '$PORT' < /etc/nginx/nginx.conf.template > /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"]
