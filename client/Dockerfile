# Nginx web server serving compiled File4Base WebDirect client
FROM nginx:alpine

# Remove default nginx html
RUN rm -rf /usr/share/nginx/html/*

# Copy custom nginx configuration with reverse proxy to server:8080
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy compiled flutter web bundle
COPY build/web /usr/share/nginx/html

# Expose port
EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
