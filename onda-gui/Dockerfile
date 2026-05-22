FROM nginx:alpine

RUN apk add --no-cache fcgiwrap spawn-fcgi bash docker-cli

COPY nginx.conf /etc/nginx/nginx.conf
COPY cgi-bin/ /usr/lib/cgi-bin/
RUN chmod +x /usr/lib/cgi-bin/*
COPY static/ /usr/share/nginx/html/

# Match host UID for /input write access + docker group for docker exec
CMD HOST_UID=$(stat -c "%u" /input 2>/dev/null || echo 1000) && \
    HOST_GID=$(stat -c "%g" /input 2>/dev/null || echo 1000) && \
    DOCKER_GID=$(stat -c "%g" /var/run/docker.sock 2>/dev/null || echo 983) && \
    addgroup -g $HOST_GID hostgroup 2>/dev/null; \
    addgroup -g $DOCKER_GID dockergroup 2>/dev/null; \
    adduser -D -u $HOST_UID -G hostgroup cgiuser 2>/dev/null; \
    addgroup cgiuser dockergroup 2>/dev/null; \
    addgroup nginx dockergroup 2>/dev/null; \
    mkdir -p /var/run/fcgiwrap && \
    spawn-fcgi -s /var/run/fcgiwrap.sock -u cgiuser -g hostgroup -U cgiuser -G hostgroup /usr/bin/fcgiwrap && \
    chmod 660 /var/run/fcgiwrap.sock && \
    chown cgiuser:nginx /var/run/fcgiwrap.sock && \
    nginx -g "daemon off;"
