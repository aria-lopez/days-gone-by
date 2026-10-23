# days-gone-by

Durable document archive, easy to self host, comes with a UI and some search tools~

Feel free to fork and change it or suggest PR's.

## System requirements

- Linux or macOS, amd64 or arm64. Windows is not supported.
- A local disk. Not NFS or SMB, file locking and SQLite do not work reliably on network drives.
- Free disk space for what you store, plus room for the database. Uploads are staged in `<data dir>/blobs/tmp` first, so the disk needs space for the biggest upload too.
- Go 1.26 or newer to build from source. No C compiler needed.
- No TLS built in, the server speaks plain HTTP. If other machines can reach it, put a TLS reverse proxy (nginx, Caddy) in front.

## Installing

## Configuring

Settings come from three places. Later ones win:

1. built-in defaults
2. `config.json`
3. `DAYS_GONE_BY_*` environment variables

### Where things live

| Env var | Default | |
|---|---|---|
| `DAYS_GONE_BY_DIR` | `~/.local/share/days-gone-by` (macOS: `~/Library/Application Support/days-gone-by`) | Data directory. Env only. `XDG_DATA_HOME` is used if set. |
| `DAYS_GONE_BY_CONFIG` | `<data dir>/config.json` | Config file path. If set, the file must exist. |

`config.json` is optional. Missing keys keep their defaults, an empty file is fine, an unknown key stops startup.

`days-gone-by config` prints the settings in effect as JSON. To start a file:
`days-gone-by config > <data dir>/config.json` (only when the file does not exist yet, it is emptied first).

### Settings

| JSON key | Env var | Default |
|---|---|---|
| `server_addr` | `DAYS_GONE_BY_SERVER_ADDR` | `127.0.0.1:8080` |
| `idle_timeout` | `DAYS_GONE_BY_IDLE_TIMEOUT` | `30s` |
| `limits.<class>.max_size` | `DAYS_GONE_BY_<CLASS>_MAX_SIZE` | see below |
| `limits.<class>.timeout` | `DAYS_GONE_BY_<CLASS>_TIMEOUT` | see below |
| `maintenance.purge_every` | `DAYS_GONE_BY_PURGE_EVERY` | `10m` |
| `maintenance.orphan_every` | `DAYS_GONE_BY_ORPHAN_EVERY` | `24h` |
| `maintenance.orphan_min_age` | `DAYS_GONE_BY_ORPHAN_MIN_AGE` | `24h` |

- `server_addr`: where to listen, `ip:port`.
- `idle_timeout`: an upload that sends nothing for this long is cut off.
- `max_size`: biggest file for that class.
- `timeout`: longest a whole upload may take.
- `purge_every`: how often the bytes of deleted files are removed.
- `orphan_every`: how often to look for files on disk that the database does not know.
- `orphan_min_age`: files younger than this are never treated as orphans. Orphans are moved to `blobs/quarantine`, never deleted.

`<class>` is `image`, `video`, `audio`, `document` or `archive` (capitals in env vars, e.g. `DAYS_GONE_BY_VIDEO_MAX_SIZE`).

| Class | `max_size` | `timeout` |
|---|---|---|
| image | `100MiB` | `10m` |
| video | `4GiB` | `2h` |
| audio | `1GiB` | `30m` |
| document | `100MiB` | `10m` |
| archive | `1GiB` | `1h` |

Sizes: `500KiB`, `100MiB`, `4GiB`, or a plain number of bytes. Max 1TiB.
Durations: `30s`, `10m`, `2h`. Must be above 0. Class `timeout` max 24h.

### Example

`config.json`:

```json
{
  "server_addr": "0.0.0.0:8080",
  "idle_timeout": "1m",
  "limits": {
    "video": { "max_size": "8GiB", "timeout": "3h" },
    "image": { "timeout": "5m" }
  },
  "maintenance": { "purge_every": "1h" }
}
```

Same thing with env vars:

```
DAYS_GONE_BY_SERVER_ADDR=0.0.0.0:8080
DAYS_GONE_BY_IDLE_TIMEOUT=1m
DAYS_GONE_BY_VIDEO_MAX_SIZE=8GiB
DAYS_GONE_BY_VIDEO_TIMEOUT=3h
DAYS_GONE_BY_IMAGE_TIMEOUT=5m
DAYS_GONE_BY_PURGE_EVERY=1h
```

## CLI Spec

`days-gone-by`

```

usage: days-gone-by <command>

commands:
  init                          create the data dir and database, and the first admin API key
  serve                         run the server
  config                        print the settings in effect as JSON, a starting point for config.json
  key new [--role R] NAME       create an API key, R is viewer, member (default) or admin
  key list                      list API keys
  key revoke ID                 revoke an API key
```

## API Spec

Base path: `/api/v1`

Every request needs `Authorization: Bearer <key>`. Make keys with `days-gone-by init` and `days-gone-by key new`.

| Role | Can |
|---|---|
| viewer | all `GET` |
| member | viewer + `POST /files`, `DELETE /files/{id}` |
| admin | member + `DELETE /blobs/{sha256}` |

Errors are plain text. Common to all routes:

| Status | Meaning |
|---|---|
| 401 | missing, invalid or revoked key (`WWW-Authenticate` is set) |
| 403 | key role too low |
| 500 | internal error |

Times are unix milliseconds. Ids are UUIDs.

The curl examples below need `curl` and `jq`, and use these:

```sh
export API=http://127.0.0.1:8080/api/v1
export KEY=dgb_...        # from `days-gone-by init` or `days-gone-by key new`
```

### POST /files

Upload a file. Role: member.

- Query: `filename` (optional, up to 255 bytes)
- Body: the raw file bytes
- Type is detected from the content, see [Accepted file types](#accepted-file-types)

`201`
```json
{
  "id": "01a16796-156a-726a-ac3b-d7b67e71d711",
  "kind": "image",
  "mime": "image/png",
  "sha256": "2ccb30cc2275833cd3c1aa9347bfd21feb36870b87dfa8d0a319c11526546e3c",
  "size": 552
}
```

| Status | Meaning |
|---|---|
| 400 | empty or unreadable body |
| 408 | upload went quiet (`idle_timeout`) or took too long (class `timeout`) |
| 413 | over the class `max_size` |
| 415 | file type not accepted, the body names the detected type |

Same bytes uploaded again get a new id and share one stored blob.

```sh
RES=$(curl -s -X POST -H "Authorization: Bearer $KEY" \
  --data-binary @cat.png "$API/files?filename=cat.png")
echo "$RES"
ID=$(echo "$RES" | jq -r .id)         # used by the examples below
SHA=$(echo "$RES" | jq -r .sha256)
```

### GET /files

List files, newest first. Role: viewer.

| Query | Default | |
|---|---|---|
| `limit` | 50 | page size, 1 to 200 |
| `after` | | start after this id, use `next` from the previous page |
| `sha256` | | only files with these bytes |

`200`
```json
{
  "files": [
    {
      "id": "01a16796-15ed-7d72-bdb4-f14471dacbad",
      "sha256": "890be7b729e982b329ae32c28b0fc28918b5e5dfc196e9363821a0b1b2c3d4e5",
      "size": 113,
      "mime": "application/zip",
      "filename": "notes.zip",
      "created_at": 1761177700000,
      "updated_at": 1761177700000
    }
  ],
  "next": "01a16796-15ed-7d72-bdb4-f14471dacbad"
}
```

Each item is the same as `GET /files/{id}`. `next` is `null` on the last page.

| Status | Meaning |
|---|---|
| 400 | bad `limit`, `after` or `sha256` |

```sh
# first page
curl -s -H "Authorization: Bearer $KEY" "$API/files?limit=20" | jq

# every file, page by page
after=""
while :; do
  page=$(curl -s -H "Authorization: Bearer $KEY" "$API/files?limit=100${after:+&after=$after}")
  echo "$page" | jq -r '.files[] | "\(.id)  \(.filename)"'
  after=$(echo "$page" | jq -r '.next // empty')
  [ -z "$after" ] && break
done
```

### GET /files/{id}

File metadata. Role: viewer.

`200`
```json
{
  "id": "01a16796-156a-726a-ac3b-d7b67e71d711",
  "sha256": "2ccb30cc2275833cd3c1aa9347bfd21feb36870b87dfa8d0a319c11526546e3c",
  "size": 552,
  "mime": "image/png",
  "filename": "cat.png",
  "created_at": 1761177600000,
  "updated_at": 1761177600000
}
```

`filename` is `null` if none was given.

| Status | Meaning |
|---|---|
| 404 | no such file, or deleted |

```sh
curl -s -H "Authorization: Bearer $KEY" "$API/files/$ID" | jq
```

### DELETE /files/{id}

Delete a file. Role: member.
The stored bytes are removed once no other file uses them.

| Status | Meaning |
|---|---|
| 204 | deleted |
| 404 | no such file, or already deleted |

```sh
# $ID and $SHA stop working after this, upload again to keep trying the examples
curl -s -X DELETE -H "Authorization: Bearer $KEY" -w '%{http_code}\n' "$API/files/$ID"
```

### GET /blobs/{sha256}

The file bytes. Role: viewer.

- `Range` is supported (`206`)
- Headers: `Content-Type`, `ETag`, `Cache-Control: immutable`, `X-Content-Type-Options: nosniff`
- Images, video, audio and PDF are shown inline. Everything else is sent with `Content-Disposition: attachment`.

| Status | Meaning |
|---|---|
| 200 / 206 | bytes |
| 404 | no such blob, or its bytes were removed |

```sh
# download
curl -s -H "Authorization: Bearer $KEY" -o cat-copy.png "$API/blobs/$SHA"

# only the first 100 bytes
curl -s -H "Authorization: Bearer $KEY" -H "Range: bytes=0-99" "$API/blobs/$SHA" | wc -c
```

### DELETE /blobs/{sha256}

Remove the bytes of a blob right now. Role: admin.

| Status | Meaning |
|---|---|
| 204 | removed |
| 404 | no such blob |
| 409 | a live file still uses it |
| 503 | uploads in progress, retry (`Retry-After: 1`) |

```sh
# KEY must belong to an admin
curl -s -X DELETE -H "Authorization: Bearer $KEY" -w '%{http_code}\n' "$API/blobs/$SHA"
```

## Accepted file types

Detected from the file content, not the filename or `Content-Type`.

| Class | Types |
|---|---|
| image | JPEG, PNG, GIF, WebP, AVIF, HEIC/HEIF, TIFF, BMP, ICO, JPEG 2000, other `image/*` |
| video | MP4, MOV, MKV, WebM, AVI, MPEG, FLV, other `video/*` |
| audio | MP3, M4A, AAC, WAV, FLAC, OGG/Opus, AIFF, other `audio/*` |
| document | PDF, TXT (and Markdown), CSV, JSON, RTF, EPUB, DOC/XLS/PPT, DOCX/XLSX/PPTX, ODT/ODS/ODP and other OpenDocument |
| archive | ZIP, GZ, TAR, BZ2, XZ, ZST, 7Z, RAR |

Not accepted (`415`): SVG, HTML, executables, and anything else not listed.

Size and time limits are per class, see [Configuring](#configuring).

## Benchmarks
