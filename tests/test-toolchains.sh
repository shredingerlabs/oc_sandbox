#!/usr/bin/env bash
# Recipe grammar, lint, and discovery (spec toolchain-sets §1/§2, ticket #195).
# Runs dist/scripts/toolchains.sh directly against fixture trees — root/env-
# parameterized, no podman needed (same fixture pattern as the other suite tests).
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="$PROJECT_ROOT/dist/scripts/toolchains.sh"
test_home="$(mktemp -d)"
trap 'rm -rf "$test_home"' EXIT

root="$test_home/toolchains"
mkdir -p "$root/recipes" "$root/custom"

write_recipe() { # <recipes|custom> <id> — recipe text on stdin
  cat > "$root/$1/$2.Dockerfile"
}

write_manifest() { cat > "$root/manifest.json"; }

discover() { bash "$helper" discover "$root"; }

unit_status() { discover | awk -F'\t' -v id="$1" '$1 == "unit" && $2 == id { print $3 }'; }
unit_field() { discover | awk -F'\t' -v id="$1" '$1 == "unit" && $2 == id { print $4 }'; }
bundle_status() { discover | awk -F'\t' -v n="$1" '$1 == "bundle" && $2 == n { print $3 }'; }
bundle_field() { discover | awk -F'\t' -v n="$1" '$1 == "bundle" && $2 == n { print $4 }'; }
warnings() { discover | awk -F'\t' '$1 == "warning" { print $2 }'; }
bundle_records() { discover | awk -F'\t' '$1 == "bundle" { print }'; }

# expect_lint_fail FILE PATTERN — lint must fail and some reason must match PATTERN
expect_lint_fail() {
  local file=$1 pattern=$2 out rc=0
  out="$(bash "$helper" lint "$file" 2>&1)" || rc=$?
  if (( rc == 0 )); then
    printf 'lint unexpectedly passed: %s\n' "$file" >&2
    exit 1
  fi
  if ! grep -qE "$pattern" <<< "$out"; then
    printf 'lint refusal does not name the expected reason (%s):\n%s\n' "$pattern" "$out" >&2
    exit 1
  fi
}

# --- 1: a valid recipe lints and discovers as a unit with its path ------------
write_recipe recipes go <<'EOF'
# go toolchain (shipped)
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends golang-go
USER dev
RUN printf 'export PATH=/usr/local/go/bin:$PATH\n' > ~/.profile.d/go.sh
EOF
[[ "$(unit_status go)" == "valid" ]]
[[ "$(unit_field go)" == "$root/recipes/go.Dockerfile" ]]
[[ "$(discover | awk -F'\t' '$1 == "unit" { print $2 }')" == "go" ]]
printf 'valid recipe discovery test passed\n'

# --- 2: '# extra:' lines are collected (absolute only) ------------------------
write_recipe recipes ros2 <<'EOF'
USER root
RUN apt-get update && apt-get install -y ros-jazzy-ros-base
# extra: /etc/apt/sources.list.d/ros2.list
# extra: /usr/share/keyrings/ros2.gpg
USER dev
EOF
extras="$(bash "$helper" extras "$root/recipes/ros2.Dockerfile")"
[[ "$extras" == $'/etc/apt/sources.list.d/ros2.list\n/usr/share/keyrings/ros2.gpg' ]]
write_recipe recipes badextra <<'EOF'
# extra: relative/path
EOF
expect_lint_fail "$root/recipes/badextra.Dockerfile" "not an absolute path"
printf 'extra collection test passed\n'

# --- 3: ARG is refused --------------------------------------------------------
write_recipe recipes argtest <<'EOF'
USER root
ARG CACHEBUST=1
RUN apt-get update
USER dev
EOF
expect_lint_fail "$root/recipes/argtest.Dockerfile" 'ARG is refused'
[[ "$(unit_status argtest)" == "invalid" ]]
grep -q 'ARG is refused' <(warnings)
printf 'ARG refusal test passed\n'

# --- 4: COPY / WORKDIR / ENTRYPOINT / unknown directives are refused ----------
for dir in COPY WORKDIR ENTRYPOINT; do
  id="${dir,,}"
  write_recipe recipes "$id" <<EOF
USER root
$dir something
USER dev
EOF
  expect_lint_fail "$root/recipes/$id.Dockerfile" "$dir is refused"
done
write_recipe recipes labeltest <<'EOF'
USER root
LABEL maintainer=x
USER dev
EOF
expect_lint_fail "$root/recipes/labeltest.Dockerfile" "unknown directive 'LABEL'"
for id in copy workdir entrypoint labeltest; do
  [[ "$(unit_status "$id")" == "invalid" ]]
done
printf 'banned/unknown directive refusal tests passed\n'

# --- 5: USER switching is monotonic root→dev ---------------------------------
write_recipe recipes switchback <<'EOF'
USER root
USER dev
USER root
EOF
expect_lint_fail "$root/recipes/switchback.Dockerfile" 'switch-back'
write_recipe recipes baduser <<'EOF'
USER someuser
EOF
expect_lint_fail "$root/recipes/baduser.Dockerfile" 'only root and dev'
printf 'USER monotonicity tests passed\n'

# --- 6: ENV PATH is refused (key=value and legacy forms) ----------------------
write_recipe recipes envpath <<'EOF'
ENV PATH=/opt/tool/bin:$PATH
EOF
expect_lint_fail "$root/recipes/envpath.Dockerfile" 'ENV PATH is refused'
write_recipe recipes envpathlegacy <<'EOF'
ENV PATH /opt/tool/bin
EOF
expect_lint_fail "$root/recipes/envpathlegacy.Dockerfile" 'ENV PATH is refused'
printf 'ENV PATH refusal test passed\n'

# --- 7: direct ~/.profile / ~/.bashrc writes are refused ----------------------
write_recipe recipes profilewrite <<'EOF'
RUN echo 'export FOO=1' >> ~/.profile
EOF
expect_lint_fail "$root/recipes/profilewrite.Dockerfile" 'refused — write a ~/.profile.d/'
write_recipe recipes bashrcwrite <<'EOF'
RUN printf 'source /opt/ros/setup.bash\n' > /home/dev/.bashrc
EOF
expect_lint_fail "$root/recipes/bashrcwrite.Dockerfile" 'refused — write a ~/.profile.d/'
printf 'profile/bashrc write refusal tests passed\n'

# --- 8: pip pins ---------------------------------------------------------------
write_recipe recipes pippin <<'EOF'
RUN pip install --no-cache-dir numpy scipy
EOF
expect_lint_fail "$root/recipes/pippin.Dockerfile" "pip package 'numpy' is not pinned"
write_recipe recipes pippin3 <<'EOF'
RUN python3 -m pip install mpremote
EOF
expect_lint_fail "$root/recipes/pippin3.Dockerfile" "pip package 'mpremote' is not pinned"
write_recipe recipes pipok <<'EOF'
RUN pip install --no-cache-dir numpy==2.2.0 scipy==1.14.1
EOF
[[ "$(unit_status pipok)" == "valid" ]]
printf 'pip pin tests passed\n'

# --- 9: npm pins ---------------------------------------------------------------
write_recipe recipes npmpin <<'EOF'
RUN npm install -g typescript jest
EOF
expect_lint_fail "$root/recipes/npmpin.Dockerfile" "npm package 'typescript' is not pinned"
write_recipe recipes npmlatest <<'EOF'
RUN npm install -g typescript@latest
EOF
expect_lint_fail "$root/recipes/npmlatest.Dockerfile" "npm package 'typescript@latest' is not pinned"
write_recipe recipes npmok <<'EOF'
RUN npm install -g typescript@5.5.4 @scope/toolkit@1.2.3 corepack@0.28.0
EOF
[[ "$(unit_status npmok)" == "valid" ]]
printf 'npm pin tests passed\n'

# --- 10: direct URLs must embed the version ----------------------------------
write_recipe recipes urllatest <<'EOF'
RUN curl -fsSL https://example.com/download/latest/tool.tar.gz | tar xz -C /opt
EOF
expect_lint_fail "$root/recipes/urllatest.Dockerfile" "floats on 'latest'"
write_recipe recipes urlapi <<'EOF'
RUN curl -fsSL "$(curl -fsSL https://api.github.com/repos/cli/cli/releases/latest | jq -r .tag_name)" -o /tmp/x
EOF
expect_lint_fail "$root/recipes/urlapi.Dockerfile" "releases/latest"
write_recipe recipes urlok <<'EOF'
RUN curl -fsSL https://example.com/download/v1.2.3/tool.tar.gz -o /tmp/tool.tar.gz
EOF
[[ "$(unit_status urlok)" == "valid" ]]
printf 'URL pin tests passed\n'

# --- 11: rustup must pin --default-toolchain ---------------------------------
write_recipe recipes rustupfloat <<'EOF'
USER dev
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --profile minimal --no-modify-path
EOF
expect_lint_fail "$root/recipes/rustupfloat.Dockerfile" 'not pinned — pass --default-toolchain'
write_recipe recipes rustupstable <<'EOF'
USER dev
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --default-toolchain stable
EOF
expect_lint_fail "$root/recipes/rustupstable.Dockerfile" "is not a pinned toolchain"
write_recipe recipes rustupdefault <<'EOF'
USER dev
RUN rustup default stable
EOF
expect_lint_fail "$root/recipes/rustupdefault.Dockerfile" 'rustup default'
write_recipe recipes rustupok <<'EOF'
USER dev
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y --profile minimal --no-modify-path --default-toolchain 1.79.0
EOF
[[ "$(unit_status rustupok)" == "valid" ]]
printf 'rustup pin tests passed\n'

# --- 12: 'base' is a reserved id ----------------------------------------------
write_recipe recipes base <<'EOF'
USER root
RUN apt-get update
EOF
[[ "$(unit_status base)" == "invalid" ]]
unit_field base | grep -q 'reserved'
printf 'reserved-id test passed\n'

# --- 13: user/shipped id collision — shipped wins with a warning --------------
write_recipe recipes rust <<'EOF'
USER dev
RUN ~/.cargo/bin/rustup --version
EOF
write_recipe custom rust <<'EOF'
USER root
COPY stuff /
EOF
[[ "$(unit_status rust)" == "valid" ]]
[[ "$(unit_field rust)" == "$root/recipes/rust.Dockerfile" ]]
grep -q 'shipped wins' <(warnings)
[[ "$(discover | awk -F'\t' '$1 == "unit" { print $2 }')" == "$(discover | awk -F'\t' '$1 == "unit" { print $2 }' | sort -u)" ]]
printf 'collision test passed\n'

# --- 14: member id without a recipe invalidates the bundle with a warning -----
write_recipe recipes node-dev <<'EOF'
RUN npm install -g typescript@5.5.4
EOF
write_manifest <<'EOF'
{ "bundles": { "web": ["node-dev", "playwright"] } }
EOF
[[ "$(bundle_status web)" == "invalid" ]]
bundle_field web | grep -q "member 'playwright' has no recipe"
grep -q "bundle 'web' is invalidated" <(warnings)
printf 'missing-member test passed\n'

# --- 15: an invalid member invalidates the bundle, naming the reason ----------
write_recipe recipes playwright <<'EOF'
USER root
COPY browsers /
EOF
[[ "$(bundle_status web)" == "invalid" ]]
bundle_field web | grep -q "member 'playwright' is invalid"
printf 'invalid-member test passed\n'

# --- 16: a valid bundle lists its members sorted ------------------------------
write_recipe recipes cpp <<'EOF'
USER root
RUN apt-get update && apt-get install -y build-essential
EOF
write_recipe recipes java <<'EOF'
USER root
RUN apt-get update && apt-get install -y openjdk-21-jdk
EOF
write_manifest <<'EOF'
{ "bundles": { "swdev": ["java", "cpp", "node-dev"], "chat": [] } }
EOF
[[ "$(bundle_status swdev)" == "valid" ]]
[[ "$(bundle_field swdev)" == 'cpp java node-dev' ]]
[[ "$(bundle_status chat)" == "valid" ]]
[[ -z "$(bundle_field chat)" ]]
printf 'valid bundle test passed\n'

# --- 17: a manifest parse failure invalidates all bundles with a warning ------
write_manifest <<'EOF'
{ "bundles": { "swdev": [
EOF
[[ -z "$(bundle_records)" ]]
grep -q 'manifest.json is missing or unreadable' <(warnings)
[[ "$(unit_status cpp)" == "valid" ]]
printf 'manifest parse-failure test passed\n'

# --- 18: discovery stays exit-0 with invalid recipes and renders the reason ---
out="$(discover)"
[[ "$(unit_status copy)" == "invalid" ]]
[[ "$(discover | awk -F'\t' '$1 == "unit" && $2 == "copy" { print $4 }')" == *"COPY is refused"* ]]
printf 'invalid-unit visibility test passed\n'

# --- 19: resolve validates ids against live discovery -------------------------
[[ "$(bash "$helper" resolve "$root" cpp)" == "$root/recipes/cpp.Dockerfile" ]]
if bash "$helper" resolve "$root" nosuchid >/dev/null 2>&1; then
  printf 'resolve accepted an unknown id\n' >&2
  exit 1
fi
nosuch_err="$(bash "$helper" resolve "$root" nosuchid 2>&1 || true)"
grep -q "unknown toolchain id 'nosuchid'" <<< "$nosuch_err"
if bash "$helper" resolve "$root" copy >/dev/null 2>&1; then
  printf 'resolve accepted an invalid id\n' >&2
  exit 1
fi
copy_err="$(bash "$helper" resolve "$root" copy 2>&1 || true)"
grep -q 'COPY is refused' <<< "$copy_err"
printf 'resolve test passed\n'

# --- 20: parse emits directive + extra records --------------------------------
write_recipe recipes parseok <<'EOF'
USER root
RUN apt-get update \
 && apt-get install -y octave
ENV OCTAVE_HOME=/opt/octave
# extra: /etc/octave.conf
USER dev
EOF
parse_out="$(bash "$helper" parse "$root/recipes/parseok.Dockerfile")"
[[ "$parse_out" == $'user\troot\nrun\tapt-get update && apt-get install -y octave\nenv\tOCTAVE_HOME=/opt/octave\nextra\t/etc/octave.conf\nuser\tdev' ]]
printf 'parse test passed\n'

# --- 21: TOOLCHAINS_ROOT env parameterization ---------------------------------
[[ "$(TOOLCHAINS_ROOT="$root" bash "$helper" discover | awk -F'\t' '$1 == "unit" && $2 == "cpp" { print $3 }')" == "valid" ]]
printf 'TOOLCHAINS_ROOT test passed\n'

printf 'all toolchains tests passed\n'
