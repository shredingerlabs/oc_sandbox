# PROTOTYPE throwaway fragment: tc-pysci — pip-only toolchain, the #181
# catalog's python-scientific member (what the matlab edition pip-installs).
# Pinned per the standing decision; exercises the pip RECORD payload path.
RUN --mount=type=cache,target=/root/.cache/pip,sharing=locked \
    pip3 install --break-system-packages \
        numpy==2.1.0 scipy==1.14.0 matplotlib==3.9.2 \
        pandas==2.2.3 openpyxl==3.1.5 pillow==11.0.0
