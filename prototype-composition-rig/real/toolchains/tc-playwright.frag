# PROTOTYPE throwaway fragment: tc-playwright — npm-global + apt toolchain, the
# #181 catalog's `playwright`. Chromium only (firefox doubles the download).
# Exercises the npm case: node_modules files are invisible to dpkg -L and pip
# RECORD, so the payload needs fragment-declared extras (tc-playwright.extras).
ENV PLAYWRIGHT_BROWSERS_PATH=/usr/local/share/playwright-browsers
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends xz-utils bzip2 && \
    rm -rf /var/lib/apt/lists/*

RUN npm install -g playwright@1.49.1

RUN npx playwright install-deps && npx playwright install chromium
