# Multi-stage build for React frontend

# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

COPY package*.json ./

RUN npm ci

COPY . .

# Hardcode /api/v1 so the built JS always routes through the nginx proxy.
# Using an inline env var on the RUN command ensures this value is baked
# into the bundle regardless of any ARG or Railway build variable.
RUN VITE_API_URL=/api/v1 npm run build

# Production stage
FROM nginx:alpine AS production

RUN apk add --no-cache curl

COPY nginx.conf.template /etc/nginx/nginx.conf.template
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN sed -i 's/\r$//' /docker-entrypoint.sh && chmod +x /docker-entrypoint.sh
COPY --from=builder /app/dist /usr/share/nginx/html

ENV PORT=3000
ENV BACKEND_URL=http://backend:8080

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${PORT}/health || exit 1

ENTRYPOINT ["/docker-entrypoint.sh"]
