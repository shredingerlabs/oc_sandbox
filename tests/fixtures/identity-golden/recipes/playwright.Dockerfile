ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright
ENV PLAYWRIGHT_SKIP_BROWSER_GC=1
RUN npm install -g playwright@1.45.0
