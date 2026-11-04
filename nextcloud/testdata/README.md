# Fixture provenance

XML and HTTP examples in `client_test.go` and `dispatch_test.go` are synthetic fixtures adapted from the [Nextcloud server 33 WebDAV manual](https://docs.nextcloud.com/server/33/developer_manual/client_apis/WebDAV/basic.html), reviewed on 2026-11-04. They are not actual service recordings. User `alice`, `cloud.local`, filenames, ETags and large file IDs are invented test values. Cases cover DAV namespace/property status, path containment, depth and native platform file transport. There is no live Nextcloud acceptance claim.
