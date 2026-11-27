# pyvisa — VISA instrument I/O via pip: the pyvisa API, the pure-python
# backend, and pyusb for the USB transport (spec toolchain-sets §3). HIL
# hardware unit; the USB stack itself rides the `usb` unit. Exact pins —
# bumps arrive via the update-flow rewrites.
USER root
RUN pip3 install --break-system-packages --no-cache-dir \
        pyvisa==1.14.1 pyvisa-py==0.7.2 pyusb==1.2.1
