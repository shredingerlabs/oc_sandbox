FROM alpine:3.21

RUN apk add --no-cache squid

COPY squid.conf /etc/squid/squid.conf
COPY allowlist.txt /etc/squid/allowlist.txt

# Scheitert laut beim Image-Build, wenn squid.conf/allowlist.txt ungueltig
# sind (squid-6.x-FATAL wie "... is a subdomain of ..." in der dstdomain-Datei)
# - statt den Container-Member erst beim Start stumm sterben zu lassen.
RUN squid -k parse

EXPOSE 3128
CMD ["squid", "-N", "-d", "1"]
