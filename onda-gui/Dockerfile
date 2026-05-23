FROM nginx:alpine

RUN apk add --no-cache python3 bash docker-cli

COPY nginx.conf /etc/nginx/nginx.conf

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

COPY onda-api.py /usr/lib/onda-api.py

# Keep cgi-bin for legacy (even though we proxy to API)
COPY cgi-bin/ /usr/lib/cgi-bin/
RUN chmod +x /usr/lib/cgi-bin/*

COPY static/ /usr/share/nginx/html/

ENTRYPOINT ["/entrypoint.sh"]
