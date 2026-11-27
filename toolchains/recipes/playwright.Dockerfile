# playwright — browser automation (spec toolchain-sets §3): the npm package,
# its system dependencies (install-deps), and the chromium + firefox browsers
# into the shared browsers dir. The module tree and the browsers are
# invisible to dpkg -L and pip RECORD — author-declared extras. The
# PLAYWRIGHT_* ENVs are literal constants (ENV PATH is lint-refused; PATH
# additions ride the ~/.profile.d hook).
# extra: /usr/lib/node_modules/playwright
# extra: /usr/lib/node_modules/playwright-core
# extra: /usr/lib/node_modules/@playwright
# extra: /usr/bin/playwright
# extra: /usr/bin/playwright-core
# extra: /usr/local/share/playwright-browsers
USER root
ENV PLAYWRIGHT_BROWSERS_PATH=/usr/local/share/playwright-browsers \
    PLAYWRIGHT_DOWNLOAD_CONNECTION_TIMEOUT=0
RUN npm install -g playwright@1.49.1
RUN npx playwright install-deps \
    && npx playwright install chromium firefox
