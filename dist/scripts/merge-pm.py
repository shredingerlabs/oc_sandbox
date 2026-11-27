#!/usr/bin/env python3
"""merge-pm.py — the set assembly's merge core (toolchain sets #200, spec §5).

COPY'd into every generated set Dockerfile and executed by the assembly's one
RUN — and executed directly by the test suite (tests/test-merge.sh) against a
fixture staging tree + fixture root, no podman needed.

The merge core turns the members' COPY'd staging trees into the composed set
filesystem, in canonical (alphabetical by member id) order:

  1. extract each member's payload archive into the target root — a contested
     path ends up with the alphabetically last member's content, matching the
     PM merge's last-canonical-writer (status matches disk because extraction
     and the status merge run in the same order);
  2. stanza-union the dpkg status files: package stanzas deduped by name, info
     entries unioned, the last canonical writer wins a contested package. The
     target root's own status is the first writer (the base seed every member
     may override). The merged status renders sorted by package name;
  3. re-emit the fragment ENVs (KEY=value pairs the builder rendered from the
     recipes' ENV directives into each member's staging envs file) into
     <ROOT>/etc/profile.d/oc-toolchains-env.sh — duplicate keys resolved
     alphabetical-last-wins, emitted sorted, shell-quoted, mode 0644.

ldconfig and the staging-tree removal stay in the RUN (ldconfig is
host-specific; the staging tree is the Dockerfile's, not the core's).

Usage:
  merge-pm.py STAGING [ROOT]

  STAGING  per-member staging tree; every direct subdirectory is one member
           (in-image: /tmp/oc-merge, one dir per member id) carrying
           payload.tar.gz, pm/ (status + info/) and — when the fragment
           declares ENVs — envs (KEY=value lines).
  ROOT     target filesystem root (default /; fixture suites pass a fixture
           root, env OC_MERGE_ROOT overrides).
"""

import os
import shutil
import sys
import tarfile


def fail(msg):
    print(f"merge-pm: {msg}", file=sys.stderr)
    sys.exit(1)


def parse_status(path):
    """Parse a dpkg status file into {package name: stanza text}.

    Stanzas are blank-line-separated and kept verbatim (whitespace-stripped).
    """
    stanzas = {}
    try:
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
    except FileNotFoundError:
        return stanzas
    for stanza in text.split("\n\n"):
        stanza = stanza.strip("\n")
        if not stanza.strip():
            continue
        name = None
        for line in stanza.splitlines():
            if line.startswith("Package: "):
                name = line[len("Package: "):].strip()
                break
        if name:
            stanzas[name] = stanza.strip()
    return stanzas


def extract_payload(member_dir, root):
    """Extract one member's payload archive into the target root."""
    payload = os.path.join(member_dir, "payload.tar.gz")
    if not os.path.isfile(payload):
        fail(f"member staging dir '{member_dir}' has no payload.tar.gz")
    try:
        with tarfile.open(payload, "r:gz") as tar:
            tar.extractall(path=root, filter="tar")
    except TypeError:  # Python < 3.12: no filter parameter
        with tarfile.open(payload, "r:gz") as tar:
            tar.extractall(path=root)


def merge_pm_state(member_dir, root, merged):
    """Fold one member's PM-state dump into the merged status + info union."""
    merged.update(parse_status(os.path.join(member_dir, "pm", "status")))
    info = os.path.join(member_dir, "pm", "info")
    if os.path.isdir(info):
        dst = os.path.join(root, "var", "lib", "dpkg", "info")
        os.makedirs(dst, exist_ok=True)
        shutil.copytree(info, dst, dirs_exist_ok=True)


def write_status(merged, root):
    """Render the merged status sorted by package name (deterministic)."""
    dst = os.path.join(root, "var", "lib", "dpkg", "status")
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    stanzas = [merged[name] for name in sorted(merged)]
    with open(dst, "w", encoding="utf-8") as fh:
        fh.write("\n\n".join(stanzas) + "\n")


def read_envs(member_dir):
    """Read a member's staging envs file into {key: value} (last line wins)."""
    pairs = {}
    path = os.path.join(member_dir, "envs")
    if not os.path.isfile(path):
        return pairs
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            pairs[key] = value
    return pairs


def shell_quote(value):
    return "'" + value.replace("'", "'\\''") + "'"


def write_envs(envs, root):
    """Re-emit the merged fragment ENVs as a profile.d shell fragment."""
    if not envs:
        return
    dst = os.path.join(root, "etc", "profile.d", "oc-toolchains-env.sh")
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    with open(dst, "w", encoding="utf-8") as fh:
        fh.write("# Generated by merge-pm.py — re-emitted toolchain fragment")
        fh.write(" ENVs (spec toolchain-sets §5).\n")
        fh.write("# Duplicate keys resolved alphabetical-last-wins; do not edit.\n")
        for key in sorted(envs):
            fh.write(f"export {key}={shell_quote(envs[key])}\n")
    os.chmod(dst, 0o644)


def main():
    if len(sys.argv) < 2 or len(sys.argv) > 3:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    staging = sys.argv[1]
    root = os.environ.get("OC_MERGE_ROOT")
    if root is None:
        root = sys.argv[2] if len(sys.argv) > 2 else "/"
    if not os.path.isdir(staging):
        fail(f"staging tree '{staging}' is not a directory")
    if not os.path.isdir(root):
        fail(f"target root '{root}' is not a directory")

    members = sorted(
        entry
        for entry in os.listdir(staging)
        if os.path.isdir(os.path.join(staging, entry))
    )

    # canonical order is derived at every use: members alphabetical by id —
    # extraction and the PM merge share this order, so status matches disk.
    merged = {}
    # the target root's own dpkg status is the first writer (the base seed)
    merged.update(parse_status(os.path.join(root, "var", "lib", "dpkg", "status")))
    envs = {}
    for member in members:
        member_dir = os.path.join(staging, member)
        extract_payload(member_dir, root)
        merge_pm_state(member_dir, root, merged)
        envs.update(read_envs(member_dir))

    if members:
        write_status(merged, root)
    write_envs(envs, root)


if __name__ == "__main__":
    main()
