FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
ENV VITE_API_URL=""
RUN npm run build

FROM nginx:1.27-alpine
RUN apk add --no-cache gettext
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/nginx.conf.template
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/
CMD ["/bin/sh", "-c", "export PORT=${PORT:-3000} ADMIN_UPSTREAM=${ADMIN_UPSTREAM:-http://admin:8081} && envsubst '$PORT $ADMIN_UPSTREAM' < /etc/nginx/nginx.conf.template > /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"]
