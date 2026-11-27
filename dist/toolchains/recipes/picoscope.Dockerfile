# picoscope — PicoTech oscilloscope support (spec toolchain-sets §3): the
# vendor apt repo, the PS2000/PS2000A/PICOIPP shared libraries, and the
# picoscope python bindings + picosdk wrapper. HIL hardware unit.
# gnupg is a build-only dep (keyring dearmor) — installed and purged inside
# the same RUN so it never enters the payload. The repo file + keyring are
# root-side /etc writes — author-declared extras. The udevadm shim is
# build-time only (removed before the layer closes).
# extra: /etc/apt/sources.list.d/picoscope.list
# extra: /usr/share/keyrings/picotech.gpg
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends gnupg \
    && curl -fsSL https://labs.picotech.com/debian/dists/picoscope/Release.gpg.key \
        | gpg --dearmor -o /usr/share/keyrings/picotech.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/picotech.gpg] https://labs.picotech.com/debian/ picoscope main" \
        > /etc/apt/sources.list.d/picoscope.list \
    && apt-get purge -y gnupg && apt-get autoremove -y --purge \
    && apt-get update \
    && apt-get download libps2000 libps2000a libpicoipp \
    && mkdir -p /etc/udev/rules.d \
    && ln -sf /bin/true /usr/local/bin/udevadm \
    && dpkg -i --force-depends libps2000*.deb libps2000a*.deb libpicoipp*.deb \
    && rm -f /usr/local/bin/udevadm \
    && rm -rf /var/lib/apt/lists/* libps2000*.deb libps2000a*.deb libpicoipp*.deb
RUN pip3 install --break-system-packages --no-cache-dir \
        picoscope==0.2.6 picosdk==1.0.0
