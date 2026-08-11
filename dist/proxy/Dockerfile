FROM alpine:3.21

RUN apk add --no-cache squid

COPY squid.conf /etc/squid/squid.conf
COPY allowlist.txt /etc/squid/allowlist.txt

EXPOSE 3128
CMD ["squid", "-N", "-d", "1"]
