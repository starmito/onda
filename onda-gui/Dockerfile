FROM nginx:alpine

# bash + fcgiwrap + docker-cli + python3 (for upload CGI)
RUN apk add --no-cache fcgiwrap spawn-fcgi bash docker-cli python3

COPY nginx.conf /etc/nginx/nginx.conf
COPY cgi-bin/ /usr/lib/cgi-bin/
RUN chmod +x /usr/lib/cgi-bin/*
COPY static/ /usr/share/nginx/html/

# Add nginx user to docker group for docker exec
CMD DOCKER_GID=$(stat -c "%g" /var/run/docker.sock 2>/dev/null || echo 983) && \
    addgroup -g $DOCKER_GID docker-host 2>/dev/null; \
    addgroup nginx docker-host 2>/dev/null; \
    spawn-fcgi -s /var/run/fcgiwrap.sock -u nginx -g nginx -U nginx -G nginx /usr/bin/fcgiwrap && \
    nginx -g "daemon off;"
