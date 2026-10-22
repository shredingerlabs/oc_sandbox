# oc-searxng image: shared SearXNG instance for all projects (see ADR 0026
# for its proxy incompatibility). The config is NOT baked in: start.sh
# bind-mounts the user-editable dist/searxng/settings.yml at runtime, so
# engine/format tweaks need no rebuild.
FROM searxng/searxng:latest
