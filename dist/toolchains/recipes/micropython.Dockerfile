# micropython — mpremote (REPL/file transfer) + esptool (Espressif flashing)
# via pip (spec toolchain-sets §3). HIL hardware unit. Exact pins — bumps
# arrive via the update-flow rewrites.
USER root
RUN pip3 install --break-system-packages --no-cache-dir \
        mpremote==1.23.0 esptool==4.8.1
