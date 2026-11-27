# node-dev — npm global dev tools: typescript, jest, pnpm, corepack,
# better-sqlite3 (spec toolchain-sets §3). The node runtime itself stays in
# the base. Globals land under /usr/lib/node_modules (nodesource npm prefix
# /usr) — invisible to dpkg -L and pip RECORD, so they are declared as
# author-declared extras for the payload dump. Versions are exact pins;
# bumps arrive via the update-flow rewrites.
# extra: /usr/lib/node_modules/typescript
# extra: /usr/lib/node_modules/jest
# extra: /usr/lib/node_modules/pnpm
# extra: /usr/lib/node_modules/corepack
# extra: /usr/lib/node_modules/better-sqlite3
# extra: /usr/bin/tsc
# extra: /usr/bin/tsserver
# extra: /usr/bin/jest
# extra: /usr/bin/pnpm
# extra: /usr/bin/pnpx
USER root
RUN npm install -g \
        typescript@5.7.2 \
        jest@29.7.2 \
        pnpm@9.15.1 \
        corepack@0.30.0 \
        better-sqlite3@11.8.1
