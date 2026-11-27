# rust — rustup with a pinned stable toolchain, minimal profile (spec
# toolchain-sets §3). Installed as dev so cargo/rustup live in /home/dev
# (~/.cargo, ~/.rustup — auto-captured by the payload dump). PATH rides the
# ~/.profile.d hook: --no-modify-path keeps ~/.profile untouched. The
# toolchain version is an exact pin — bumps arrive via the update-flow
# rewrite, never floating.
USER dev
RUN mkdir -p ~/.profile.d \
    && curl -fsSL https://sh.rustup.rs | sh -s -- -y --no-modify-path --profile minimal --default-toolchain 1.90.0
RUN printf 'export PATH="$HOME/.cargo/bin:$PATH"\n' > ~/.profile.d/rust.sh
