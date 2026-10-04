# synology — NAS file-change watching plugin

Watches a NAS volume directory for file changes (created/modified/deleted) and pushes
events to the platform to trigger workflows; paired with read_file / list_dir / move_file
/ delete_file operations to complete the "ingest → process → archive" loop.

## Architecture and credential model

- **Credential = watch config**: `path` (subdirectory inside the container) +
  `include` (extension filter) + `ignore` (extra ignores) + `settle_seconds` (settle time
  in seconds). **One credential = one watched directory = one source instance** (an
  in-process goroutine).
- Multiple directories → create multiple credentials; a single container handles all of
  them; in a multi-container deployment, the platform automatically shards delivery by
  credential (the per-credential source architecture).
- Events carry only metadata (path/name/ext/dir/size/mtime + the automatically attached
  credential_id); workflows use `read_file` to fetch the file as needed (producing a
  platform file reference that plugs directly into PDF parsing / LLM file inputs).
- All operation paths are jailed inside the credential's `path`, and out-of-bounds access
  is rejected.

## Events

| Event | Fires when | Dedicated fields |
|------|---------|---------|
| file_created | after a new file has **settled** (quiet for `settle` seconds with a stable size) | size / mtime |
| file_changed | an existing file is rewritten and has settled | size / mtime |
| file_deleted | a file is deleted or moved out of the watched tree | — |

Common fields: `path` / `name` / `ext` / `dir` (+ the platform-flattened `credential_id` —
used by reply/processing nodes to bind their dynamic credential).

Why settle detection matters: an SMB client save is temp-file+rename, a large file copy is
create+continuous-write; without waiting for it to settle, the workflow would get a
half-written file. Defaults to 2 seconds; bump it up in the credential for directories
that receive large file copies.

Built-in ignores (always active): `@eaDir`, `#recycle`, `#snapshot`, `@*` system
directories, `._*` (AppleDouble), `~$*` (Office locks), `Thumbs.db`, `.DS_Store`,
`*.tmp/.part/.crdownload`, and the like.

## Synology must-dos (gotcha checklist)

1. **The container must run on the NAS itself** (Container Manager). inotify doesn't
   cross network filesystems: mounting NFS/SMB on another machine and then bind-mounting
   it won't deliver events; running it on the NAS itself works because when another
   computer writes over SMB, smbd writes to disk locally and the event fires normally.
2. **Raise the inotify watch limit**: DSM defaults to `fs.inotify.max_user_watches=8192`,
   which gets exhausted once the directory tree grows, and fails silently by default (this
   plugin surfaces an error on the credential status for this). `/etc/sysctl.conf` gets
   reset by DSM, so use **Control Panel → Task Scheduler → Create → Triggered task
   (Boot-up) → root** to run:
   ```
   sh -c '(sleep 90 && sysctl -w fs.inotify.max_user_watches=1048576) &'
   ```
   (The sleep avoids being overwritten by DSM's own startup initialization.) After
   creating it, run it manually once to take effect immediately.
3. **Permissions**: files on the volume are owned by a DSM user. The container defaults
   to root, which works fine; to lock it down, set `user: "<uid>:<gid>"` in the compose
   file to an account with read/write access to that shared folder.
4. Avoid pointing the watched directory at the whole `/volume1` (too much junk, watch
   count explodes); create a credential per business subdirectory instead.

## Deployment

```bash
# 1) Platform side: create a "synology" entry in plugin management (custom / nats transport), get the default group's token
# 2) Build the image (from the repo root)
docker build -f plugin-builtin/synology/Dockerfile -t synology:latest .
# 3) Adjust env/mounts based on the docker-compose.yml sample, start the project in Container Manager
# 4) Platform credential management: create a credential for synology, with path set to the in-container path (e.g. /watch/research-intake)
# 5) Canvas: on the event trigger node, pick synology + subscribe to file_created → downstream read_file (bind the credential's "variable" to the trigering credential_id)
```

## Local dev smoke test

```bash
SOKEL_ENDPOINT=https://<platform-address> SOKEL_TOKEN=skp_xxx SOKEL_INSTANCE_ID=nas-dev go run .
# point the credential's path at any local directory, drop files into it, and watch the event log
```

Testing: `go test -race ./...` (includes settle/ignore/dynamic-subdirectory cases against
a real filesystem).
