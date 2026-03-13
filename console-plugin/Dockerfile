# Stage 1: Build frontend
FROM registry.access.redhat.com/ubi9/nodejs-18:latest AS frontend-builder
USER 0
WORKDIR /opt/app-root/src
COPY package.json package-lock.json ./
RUN npm ci
COPY src/ src/
COPY console-extensions.json tsconfig.json webpack.config.ts ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.24 AS backend-builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY pkg/ pkg/
RUN CGO_ENABLED=0 GOOS=linux go build -a -o backend ./cmd/backend/

# Stage 3: Runtime
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

RUN microdnf install -y nginx && microdnf clean all

# Copy static assets for nginx
COPY --from=frontend-builder /opt/app-root/src/dist /usr/share/nginx/html

# Copy backend binary
COPY --from=backend-builder /workspace/backend /usr/local/bin/backend

# nginx dirs need to be writable by non-root
RUN mkdir -p /var/run/nginx /var/log/nginx /var/lib/nginx/tmp && \
    chown -R 1001:0 /var/run/nginx /var/log/nginx /var/lib/nginx /usr/share/nginx/html && \
    chmod -R g=u /var/run/nginx /var/log/nginx /var/lib/nginx /usr/share/nginx/html

USER 1001

EXPOSE 9443 9444

# Default: run nginx (backend container overrides with /usr/local/bin/backend)
CMD ["nginx", "-g", "daemon off;"]
