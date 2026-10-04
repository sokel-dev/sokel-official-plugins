#!/bin/sh
# drop-root: run the container as non-root (uid 65532) while existing deployments upgrade transparently.
#
# Old images ran as root, so mounted volumes (server's /data, plugins' /var/lib/sokel) are root-owned;
# setting USER 65532 in the Dockerfile would hit permission denied on the first write after upgrading. So the entrypoint still starts as root:
# it fixes ownership of mismatched directories in SOKEL_OWN_DIRS once (checks only the top level; once it matches it doesn't recurse again), then execs via su-exec with dropped privileges.
# When the orchestrator already specifies a non-root user (docker --user / k8s runAsUser), ownership is left alone and the command starts directly.
set -e
RUN_UID=65532
if [ "$(id -u)" = "0" ]; then
  for d in $SOKEL_OWN_DIRS; do
    mkdir -p "$d"
    if [ "$(stat -c %u "$d")" != "$RUN_UID" ]; then
      chown -R "$RUN_UID:$RUN_UID" "$d"
    fi
  done
  exec su-exec "$RUN_UID:$RUN_UID" "$@"
fi
exec "$@"
