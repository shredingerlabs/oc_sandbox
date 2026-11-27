# usb — USB bus tools + libusb via apt (spec toolchain-sets §3); shared by
# the hardware units (picoscope, pyvisa, esptool, …). HIL hardware unit.
# apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        usbutils libusb-1.0-0 \
    && rm -rf /var/lib/apt/lists/*
