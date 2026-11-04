# Synology upload

Upload workflow files into a Synology NAS folder through DSM's own FileStation API, so Synology Drive, Cloud Sync, and file indexing can detect new files normally.

## Difference from the Synology file watcher plugin

Synology file watcher works through mounted directories: its container runs on the NAS and reads or writes mounted paths directly. **Writing files this way has a pitfall**:
the Synology host does not receive change notifications for files written by the container, so **Synology Drive / ShareSync will not synchronize them**.
The files exist on disk but may not be discovered until someone touches them on the host or restarts Drive.

This plugin instead logs in with a DSM account and uploads through the web interface, equivalent to uploading in File Station, avoiding that issue.
It also requires no directory mount and can run anywhere that can reach the NAS.

Use Synology file watcher to watch for new NAS files. Use this plugin to put files onto the NAS.

## Deployment

The plugin process must be able to reach the DSM address, over a LAN or the public internet. It can run in the NAS's Container Manager or another machine on the same network.
Set the platform URL and connection token through `SOKEL_ENDPOINT` / `SOKEL_TOKEN` environment variables.

## Credentials

| Field | Description |
|---|---|
| **DSM URL** | The address used to open DSM in a browser, including scheme and port, such as `https://nas.example.com:5001` or `http://nas.local:5000`. |
| **Username** | DSM account. |
| **Password** | The account password, stored encrypted. |
| Skip certificate verification | `true` disables HTTPS certificate verification. **Use only for a self-signed DSM certificate on a LAN**; leave empty for public-internet connections. |

Account recommendations:

- **Create a dedicated account** and grant read/write access only to the target folder under Control Panel → Shared Folder. The plugin only needs uploads and should not use an administrator account.
- **Two-step verification must be disabled for this account**, because API login cannot enter a verification code. If DSM enforces it for all users, exempt this account separately.
- Grant the account File Station application permission under Control Panel → Permissions → File Station.

After configuration, click Check credentials to perform a login with the username and password. Failures distinguish an unreachable URL, an untrusted certificate,
an incorrect password, and two-step verification issues.

## Upload files

| Input | Description |
|---|---|
| **Files** | One or more platform files, saved under their original filenames. |
| **Destination folder** | The path shown in DSM, **starting with the shared folder**, such as `/research/入库/2026`. Do not use the SSH/container path `/volume1/...`. |
| Existing filename | Error, the default; overwrite; or skip. Skip does not upload existing files and marks them as skipped in the result. |
| Create folders automatically | Creates missing destination folders recursively; enabled by default. |

The `files` output corresponds one-to-one with the input files. Each entry contains the filename, full NAS path, size, and whether it was skipped.
Separate outputs give uploaded and skipped counts.

## Notes

- If an upload fails midway through a multi-file request, **files already uploaded remain on the NAS**. The error identifies the file where processing stopped and the prior completed count.
  On rerun, choose Skip for existing filenames to avoid errors on files already uploaded.
- Each file is read entirely into the plugin process before uploading. Keep individual files below a few hundred MB where possible.
- Each invocation logs in once and logs out after uploading; sessions are not retained.
