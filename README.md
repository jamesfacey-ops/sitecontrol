# SiteControl

A gate log and evacuation muster for remote site-control posts. Gate officers sign
people and vehicles in and out; dispatch sends vehicles between posts with a timed
overdue alert; and at any moment the muster shows who is on site, ready to print
during an evacuation.

It replaces a single Microsoft Access database. Each post runs one self-contained
program (`sitectl.exe` on Windows, `sitectl` on Linux) with its own local database,
and keeps working with no network at all. When posts can reach each other they
share records, so a person checked in at one post can be found at any other.

> **Status: prototype.** It runs as a five-post live demo. It is not yet in
> production at a real gate.

**To install it at a site, follow [INSTALL.md](INSTALL.md).**

## Contents

- [Installing at a site](INSTALL.md)
- [Documentation](#documentation)
- [How it works](#how-it-works)
- [Security and data, for IT](#security-and-data-for-it)
- [Try it on one Windows PC](#try-it-on-one-windows-pc)
- [Install a post as a Windows service](#install-a-post-as-a-windows-service)
- [Install a post on Linux](#install-a-post-on-linux)
- [Build from source](#build-from-source)
- [Repository layout](#repository-layout)

## Documentation

The standard operating procedures are in [`docs/guides`](docs/guides), as PDF and Word:

| Guide | For |
|---|---|
| [Site Operator Guide](docs/guides/SiteControl_Site_Operator_Guide.pdf) | Gate officers at a post |
| [Web Operator Guide](docs/guides/SiteControl_Web_Operator_Guide.pdf) | Officers using the web pages |
| [Dispatch Guide](docs/guides/SiteControl_Dispatch_Guide.pdf) | Dispatch: vehicle runs, overdue alerts, reports |
| [Administrator Guide](docs/guides/SiteControl_Administrator_Guide.pdf) | IT: installing posts, new sites, passwords and keys, backups, restores |

The guides were written against the live demo. Their screenshots show generated
demo data, not real people.

## How it works

- **One program per post.** Each post runs its own copy of `sitectl` with its own
  config file (`site.toml`) and its own SQLite database. Officers use it in an
  ordinary web browser on that machine. Recording never needs the network.
- **Posts sync directly with each other.** Each post lists every other post as a
  peer and pulls their new records every few seconds (normally 10). There is no
  central server. When a link drops, both sides keep working and catch up when
  it comes back.
- **Records are never overwritten.** Every check-in, departure and correction is
  an event appended to that post's own log, and a post only ever writes its own
  log. Screens are rebuilt from those events the same way on every post, so posts
  that have the same events show the same thing. Nothing is decided by "last
  write wins".
- **Standalone or linked.** Where sites have no network link, each post is
  installed **standalone**: it makes no connections, opens no port, and never shows
  a "lost signal" warning. Vehicles leaving for other sites are still recorded,
  without a timer. Where a private link exists, posts can be **linked** to share
  records. See [INSTALL.md](INSTALL.md#choose-a-setup).
- **Vehicle runs between linked posts are timed.** When a vehicle is dispatched to another gated post, a
  countdown starts. If the vehicle hasn't checked in at the other end in time,
  both posts raise an overdue alert. The alert survives a restart or power cut.

`design/sync-protocol-v1.md` describes the sync protocol in detail.

## Security and data, for IT

- **No cloud, no accounts, no telemetry.** The program connects out to only one
  thing: the other posts' addresses listed in its own config file (`base_url` of
  each `[[peers]]` entry). A standalone post has none, so it makes no connections. The admin `swap` tools also contact a post address you
  give them. Nothing else in the source makes an outbound connection.
- **Self-contained web pages.** Pages are served by the program itself and load
  nothing from the internet: no external scripts, fonts or stylesheets.
- **One program file, no runtime.** It's written in Go with CGO disabled and a
  pure-Go SQLite driver (`modernc.org/sqlite`). It needs no installer, runtime,
  database server or other software. Dependencies are listed in `go.mod`.
- **Login.** Each post has one site password that officers type. There are no
  usernames. The password is generated, never typed into a plan.
- **Post-to-post authentication.** Each post accepts a 64-hex-character peer key,
  and every other post holds that key. Traffic between posts is plain HTTP, so
  posts must be linked over a private, encrypted network (a VPN or WireGuard),
  never the open internet.
- **Data at rest.** The database holds personnel names and phone numbers. On
  Windows, `install-service.ps1` restricts `C:\ProgramData\SiteControl\` to
  Administrators, SYSTEM and the service's own account. Full-disk encryption
  (e.g. BitLocker) is recommended. Keep the folder out of OneDrive or
  network-share sync.
- **Least privilege.** The Windows service runs as a virtual service account
  (`NT SERVICE\SiteControl`) with no password to manage. The Linux unit runs
  sandboxed (`ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`).
- **Not code-signed.** If application control (AppLocker, WDAC, SmartScreen)
  is in use, it needs an allow rule. A folder-path rule survives updates; a
  file-hash rule has to be renewed for each new version.

## Try it on one Windows PC

This runs two posts on one PC so you can see them sync. No Go install is needed.
Commands are run in PowerShell from the repository folder.

1. Get this repository (`git clone`, or **Code → Download ZIP** and extract it).
   Download `sitectl.exe` from the [latest release](../../releases/latest) into the
   repository folder and check its SHA-256 against `SHA256SUMS.txt` on the release:
   ```powershell
   Get-FileHash .\sitectl.exe -Algorithm SHA256
   ```
2. Generate the two posts' configs from the example plan:
   ```powershell
   .\sitectl.exe admin season -plan deploy\examples\site-plan.example.toml -out C:\SiteControl-season
   ```
   It writes `alpha\site.toml`, `beta\site.toml`, `passwords.txt` (one login
   password per post) and `IT-CHECKLIST.txt`.
3. Install **alpha** as a Windows service (see the next section), or run it in a
   console window. Its database is set to `C:\ProgramData\SiteControl\alpha.db`.
   The program creates the database but not its folder, so when running by hand
   create the folder first (the service installer does this for you):
   ```powershell
   New-Item -ItemType Directory -Force C:\ProgramData\SiteControl
   .\sitectl.exe -config C:\SiteControl-season\alpha\site.toml
   ```
4. In a second console window, run **beta** (its database is created in the
   folder you start it from):
   ```powershell
   .\sitectl.exe -config C:\SiteControl-season\beta\site.toml
   ```
5. Open <http://127.0.0.1:8091> (Alpha Gate) and <http://127.0.0.1:8092> (Beta
   Gate). Log in with each post's password from `passwords.txt`.
   <http://127.0.0.1:8091/status> needs no login and shows sync health.
6. At Alpha, open the roster page (<http://127.0.0.1:8091/roster>) and enrol a
   test person. Both example posts have `orientation = true`, so a post refuses
   to check in anyone who isn't enrolled, and enrolling at Alpha records Alpha's
   orientation. Check the person in at Alpha's gate.
7. On Alpha's operator console (<http://127.0.0.1:8091/trips>), dispatch a vehicle
   to Beta Gate. Within about 10 seconds the vehicle appears on Beta's board
   (<http://127.0.0.1:8092/trips>) with its countdown.
8. Record the arrival at Beta. The run closes at both posts. The person is only
   entered at Beta if they also hold Beta's orientation; otherwise Beta records
   the arrival and says the person was not entered.

A fresh install starts with an empty database. There is no demo data.

## Install a post as a Windows service

From an **elevated** PowerShell (Run as Administrator), in the repository folder
with `sitectl.exe` in it:

```powershell
Set-ExecutionPolicy -Scope Process Bypass   # this window only; Windows blocks scripts by default
Get-ChildItem -Recurse | Unblock-File       # only needed if you downloaded the ZIP
.\deploy\windows\install-service.ps1 -SourceExe .\sitectl.exe -ConfigSource C:\SiteControl-season\alpha\site.toml
```

This:

- copies the program to `C:\Program Files\SiteControl\sitectl.exe`;
- copies the config to `C:\ProgramData\SiteControl\config.toml` (note the file
  name: `config.toml`);
- locks that folder down;
- registers the `SiteControl` service (automatic start, restart on failure);
- starts the service.

`db_path` in the config **must be an absolute path** under
`C:\ProgramData\SiteControl\`, because a Windows service has no fixed working
folder. Nightly backups go to a `backups` folder beside the database.

When editing a config by hand, put Windows paths in **single quotes**
(`db_path = 'C:\ProgramData\SiteControl\alpha.db'`). Inside double quotes, TOML
reads backslashes as escape characters, and the post refuses to start.

Delete the season output folder once every post is installed. It is the only
other copy of every key.

To remove the service:

```powershell
.\deploy\windows\uninstall-service.ps1             # keeps the data
.\deploy\windows\uninstall-service.ps1 -RemoveData # also deletes C:\ProgramData\SiteControl
```

For real posts on separate laptops, allow the post's port inbound in Windows
Defender Firewall only from the other posts' addresses (listed in
`IT-CHECKLIST.txt`).

**Known Windows limitation:** the overdue timer uses system uptime as a second clock,
so that a change to the system time is flagged for review instead of silently
moving a countdown. That second clock is read on Linux only. On Windows the timer
works and survives restarts, but a manual change to the system time can shift a
running countdown without a flag. Keep Windows time sync on.

## Install a post on Linux

`deploy/sitectl.service` is a hardened systemd unit for one post in `/opt/sitectl`.
Setup steps are in its header comments and in the Administrator Guide.

## Build from source

Requires Go 1.25.6 or later.

```powershell
# Windows
$env:CGO_ENABLED="0"; go build -trimpath -ldflags "-s -w" -o sitectl.exe ./cmd/sitectl
```
```sh
# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o sitectl ./cmd/sitectl
```

`go vet ./...` is clean. Tests: `go test ./...`. See [Test status](#test-status).

### Test status

`go vet ./...` is clean. There are about 2,100 tests. Some fail **on purpose** or
for known reasons:

- **Linux** (what the demo runs on): 16 fail. 14 are *declared reds*: tests
  deliberately left failing to keep an open design question visible. Each is listed
  with its reason in [`DECLARED-REDS.md`](DECLARED-REDS.md). The other 2 check
  internal planning documents that aren't part of this repository.
- **Windows**: 31 fail:
  - the declared reds (more of them fail on Windows);
  - the same 2 planning-document tests;
  - 5 where the test itself doesn't suit Windows: it writes Windows paths into TOML
    with backslash escapes, expects Unix file permissions, or reads Linux uptime;
  - 1 timing-sensitive test (`TestD16PeerEventAheadOfThisClockShowsTheSkew`) that
    also fails intermittently on the original source.

## Repository layout

| Path | What |
|---|---|
| `cmd/sitectl` | The program: web server, sync, backup/restore, and the `admin` commands |
| `cmd/dumpcompare` | Operator tool: proves two posts hold identical data |
| `cmd/dupreport` | Operator tool: finds one person on site under two IDs (a muster over-count) |
| `internal/` | Domain model, event store, sync engine, overdue deadlines, web pages |
| `deploy/windows/` | Windows service install / uninstall scripts |
| `deploy/sitectl.service` | Linux systemd unit |
| `INSTALL.md` | Step-by-step install at a site (standalone or linked) |
| `deploy/examples/` | Example site plans (no secrets): standalone post, and a two-post trial |
| `deploy/BACKUP-RESTORE.md` | Backup and restore procedure |
| `docs/guides/` | Operator, dispatch and administrator guides |
| `design/` | Sync protocol specification |
| `DECLARED-REDS.md` | Tests deliberately left failing, each with its reason |

## Provenance

This repository is a single snapshot of the source that the live demo runs:
development commit `bdda1d5` (tag `deploy/demo-2026-10-07a`), built 2026-10-07.
Development history, internal build reports and demo configuration were left
out. No program code has been changed: the published source builds a Linux binary
byte-identical to one built from the deployed commit. The only test changes:
tests that existed solely to check the removed demo files were removed with them.
