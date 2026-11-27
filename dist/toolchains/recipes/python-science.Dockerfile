# python-science — the scientific python stack via pip: numpy, scipy,
# matplotlib, pandas, openpyxl, pillow (spec toolchain-sets §3); shared by
# the matlab and latex bundles. Exact pins — bumps arrive via the
# update-flow rewrites.
USER root
RUN pip3 install --break-system-packages --no-cache-dir \
        numpy==2.1.0 scipy==1.14.0 matplotlib==3.9.2 \
        pandas==2.2.3 openpyxl==3.1.5 pillow==11.0.0
