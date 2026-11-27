USER dev
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
 | sh -s -- -y --profile minimal --default-toolchain 1.79.0 --no-modify-path
RUN printf 'export PATH="$HOME/.cargo/bin:$PATH"\n' > ~/.profile.d/rust.sh
