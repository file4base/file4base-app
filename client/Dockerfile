# Multi-stage Dockerfile for the File4Base Web Client.
# Stage 1 compiles the Flutter web bundle, stage 2 serves it with Nginx, so
# `docker compose up` works on a clean clone without a local Flutter SDK.

# ---- Stage 1: build the Flutter web bundle ----
# The web bundle is platform independent: build it once on the build host's
# platform and reuse it for every target platform of the Nginx image.
FROM --platform=$BUILDPLATFORM ghcr.io/cirruslabs/flutter:stable AS builder

WORKDIR /app

# Cache pub dependencies
COPY pubspec.yaml pubspec.lock ./
RUN flutter pub get

# Copy sources and build
COPY . .
RUN flutter build web --release

# ---- Stage 2: Nginx web server serving the compiled bundle ----
FROM nginx:alpine

# Remove default nginx html
RUN rm -rf /usr/share/nginx/html/*

# Copy custom nginx configuration with reverse proxy to server:8080
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy compiled flutter web bundle
COPY --from=builder /app/build/web /usr/share/nginx/html

# Expose port
EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
