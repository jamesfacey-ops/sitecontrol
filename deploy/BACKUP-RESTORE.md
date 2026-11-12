# Nightly backup and restore (FIX16 D, James 2026-10-06)

Each post backs up its own database every night, by itself. Nothing to
install: the timer runs inside `sitectl`, the same on a Windows gate laptop
and on Linux. This page is what an operator needs to check it and to restore
from it.

## What it does

- **When:** every night at `backup_time` (local). With no `backup_time`
  line a post takes it at `03:00` **plus a few minutes of its own** (0-14,
  worked out from its `site_id`, the same every night): posts that share one
  host and disk (CT510, the staging stack) then do not all copy at once —
  at 03:00 together they slowed every gate for two minutes (sc-stress F19).
  For the staging/CT510 ids: npf 03:00, ndb 03:01, ice-pad 03:08, sid 03:11,
  ice-road 03:14. `/status` and the log show the time each copy was taken.
  A `backup_time` line is followed exactly, with no offset. A post
  that starts with no copy from the last 24 hours (a new post, or a laptop
  that was off at 03:00) takes one 2 minutes after starting. The timer reads
  the wall clock every 30 s, so a laptop that slept through 03:00 takes the
  copy when it wakes instead of skipping the night.
- **How:** SQLite `VACUUM INTO` on its own connection, which gives one
  consistent snapshot while the post keeps working. It is never a raw file copy
  (a live WAL database is three files, and a copy of them taken mid-write is
  not a database). The post's writer is never blocked. A season-sized database
  takes seconds.
- **Checked before it counts:** each copy is written as `….partial`, then
  opened read-only and checked with `PRAGMA integrity_check`, every event's
  hash (the same pass the post runs on start) and the `schema_version`.
  Only a copy that passes gets its final name:
  `<db-name>-backup-YYYYMMDD-HHMMSS.db`. A file with that name is a verified
  copy.
- **Keeps** the 7 newest copies. Older ones are deleted only after a new copy
  has passed its check. Other files in the folder are never touched.
- **Second copy (USB):** if `backup_mirror_dir` is set, each verified copy is
  also written there and checked there, and that folder keeps its own 7. If
  that write fails (for example the drive is unplugged), the main copy still
  counts, and `/status` turns amber and gives the reason.

## Config (all optional)

```toml
backup_dir        = "C:\\ProgramData\\SiteControl\\backups"  # default: a "backups" folder beside db_path
backup_mirror_dir = "E:\\SiteControl-backups"                # default: none
backup_time       = "03:00"                                  # default: 03:00 + 0-14 min by site_id; set = exactly this, 24-hour local
```

⚠ **Ship the binary before you add any of these lines.** `sitectl` refuses
unknown keys, so an older binary will not start on a toml that has them (the
same rule applied to fix15's `road_work_label`). An old toml with none of
these lines starts unchanged on the new binary.

- **Linux (systemd):** the unit's sandbox (`ProtectSystem=strict`,
  `ReadWritePaths=/opt/sitectl`) allows writes only inside `/opt/sitectl`.
  The default folder `/opt/sitectl/backups` is inside it. A
  `backup_mirror_dir` anywhere else also needs a `ReadWritePaths=` line in
  the unit, or every second copy fails with "read-only file system".
- **Windows:** the default folder inherits `C:\ProgramData\SiteControl`'s
  locked-down ACL. A USB drive formatted NTFS has to grant the service
  account (`NT SERVICE\SiteControl`) Modify. exFAT and FAT32 have no ACLs and
  work as they are.

## Checking it

On the post's `/status`:

- A **Backup tile** shows the state in one word: `OK`, `OLD`, `FAILED`,
  `MISSING` or `NONE YET`.
- A **Backup line** in the table shows the newest verified copy's time, age,
  file name and folder.
- **Amber** means the newest copy is over 30 hours old (last night's backup
  did not happen), or the second copy failed.
- **Red** means the newest copy is over 36 hours old, or the last attempt
  failed. The reason is given in plain words, for example `cannot create the
  backup folder …`.
- The journal / service log has one line per attempt: `backup: verified
  copy …` or `backup: FAILED: …`.

The tile is not shown when the page is opened through the dispatch host.

To take a copy by hand (safe while the post is running):

```
sitectl -config <site.toml> -backup <folder>
```

## Restore

`sitectl -restore <file>` replaces the post's database with a copy. It
refuses, and changes nothing, unless all of these hold:

1. **The post is stopped.** Its `listen_addr` is free, and no process has the
   database open.
2. **The copy passes the same check** it passed when it was taken.
3. **`node_epoch` in the config is above every epoch this post has written
   in**, both in the copy and in the database being replaced (when that can
   still be read).

The database being replaced is never deleted. It is renamed to
`<db>.pre-restore-<stamp>`, along with its `-wal`/`-shm`.

### ⚠ The gotcha: the restored post is behind its peers

A copy from last night is missing everything this post wrote since then, but
its peers already hold those events. **On start, the post rejoins.** Before
it serves anything, it pulls its own newer events back from the peers, then
theirs. So the data lost since the backup comes back from the peers, provided
a peer is reachable.

This is also why rule 3 exists. The peers already hold this post's events
under the current epoch, numbered past the end of the copy. If the restored
post wrote new events under that same epoch, they would take sequence
numbers the peers already hold for different events. The peers refuse that
permanently (`prefix_diverged`). Bumping `node_epoch` is the re-image rule
(design §8.2/§8.6), the same step as wiping a post. The old epoch's newer
events still come back from the peers, and new writes start a fresh stream.

If **no peer is reachable** when the restored post starts, it serves with
only the copy's data. The newer events arrive when the link returns (each
sync cycle retries, and `/status` shows `self-history pending` until they
have arrived).

### Windows (gate laptop), elevated PowerShell

```powershell
Stop-Service SiteControl
# 1. Edit C:\ProgramData\SiteControl\config.toml: node_epoch = <current + 1>
# 2. Restore (pick the copy; newest is last):
dir C:\ProgramData\SiteControl\backups
& "C:\Program Files\SiteControl\sitectl.exe" -config C:\ProgramData\SiteControl\config.toml `
    -restore C:\ProgramData\SiteControl\backups\<site>-backup-YYYYMMDD-HHMMSS.db
Start-Service SiteControl
```

### Linux (systemd)

```sh
systemctl stop sitectl
cd /opt/sitectl               # db_path is relative to here (D-85)
# 1. Edit site.toml: node_epoch = <current + 1>
ls backups/
./sitectl -config site.toml -restore backups/<site>-backup-YYYYMMDD-HHMMSS.db
systemctl start sitectl
journalctl -u sitectl -f       # expect REJOIN lines, no FAILED
```

### After either one

Open `/status`. Every peer should read OK, nothing should be `self-history
pending`, and `node_epoch` should show the new value. On a peer's `/status`,
this post's streams should show both the old epoch (now at the peer's head)
and the new one.
