# Synology file watcher

Watch NAS directories and trigger workflows when files are added, changed, or deleted. You can also read files, list directories, move files, and delete files.

## Deployment: this plugin must run on the NAS

The plugin detects changes through inotify, and **inotify does not work across network filesystems**. The plugin must therefore run on the same machine as the files:

1. Create a container from this repository's image in Synology Container Manager.
2. **Bind-mount** the volume directory to watch into the container, such as `/volume1/research` → `/watch`.
3. Set the platform URL and connection token through `SOKEL_ENDPOINT` / `SOKEL_TOKEN` environment variables.

When Windows or Mac users write through SMB, the NAS's smbd writes locally, so events still fire normally.
See the README in the plugin directory for details, including Synology inotify watch-limit adjustments.

## Credential = watch configuration

One credential represents one watched subdirectory. Create multiple credentials for multiple directories:

| Field | Description |
|---|---|
| **Watch directory** | The path **inside the container**, beneath the mount point, such as `/watch/研报入库`. |
| Extension filter | Comma-separated, such as `pdf,docx`. Empty means all extensions. |
| Ignore patterns | Comma-separated substrings. Synology housekeeping directories such as `@eaDir` and `#recycle` are always ignored. |
| Settle seconds | Report only after a file has been quiet for N seconds and its size has stabilized. Defaults to 2. |

**Settle seconds are necessary**. SMB may create a temporary file and rename it when copying a large file.
Reporting halfway through the copy would make downstream operations read an incomplete file.

After configuration, click Check credentials. It verifies that the watch directory exists and is readable by the container, and reports the number of immediate entries.
A missing mount or a host path mistakenly entered as a container path is detected here.
Otherwise, the symptom would simply be an event source that never emits events, which is much harder to diagnose.

## Notes

- Every operation's path is **restricted to the credential's watch directory**, preventing reads or writes elsewhere in the mounted volume.
- Events contain only metadata such as path, size, and time, not file bytes. Use Read file when content is needed.
  NAS files are often videos or archives; multi-gigabyte payloads should not be placed in events.
