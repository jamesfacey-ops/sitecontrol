# Installing SiteControl at a site

Step by step, for the Windows laptop at one site-control post. Allow about 20 minutes.
You need administrator rights once, during the install. Gate officers never need them.

- [Choose a setup](#choose-a-setup)
- [Standalone install (no link between sites)](#standalone-install-no-link-between-sites)
- [After installing](#after-installing)
- [Linked install (sites share records)](#linked-install-sites-share-records)
- [Updating, password changes, removal](#updating-password-changes-removal)
- [Troubleshooting](#troubleshooting)

## Choose a setup

| | **Standalone** | **Linked** |
|---|---|---|
| Use when | The sites have **no network link** to each other | The sites share a private link (VPN, WireGuard or similar) |
| Network | None. Makes no connections and opens no port. Only this laptop's browser reaches it | One TCP port open between the posts, over the private link |
| Warnings | **Never** shows "LOST SIGNAL", because there is no other site to lose | Shows "LOST SIGNAL" when the link drops (records catch up when it returns) |
| Vehicles to other sites | Recorded as leaving ("Record run to Pad Post — no gate there, no timer"). No overdue timer, because the arrival can't reach this post | Timed. An overdue alert fires at both posts if the vehicle doesn't arrive in time |
| Person lookup across sites | No. Each site has its own list | Yes |

Choose **Standalone** unless the private link is already approved and working.

Linking standalone sites later has **not been tested yet**. In principle it can be
done: each post keeps its own records, and linked posts copy each other's history.
Try it on spare laptops before doing it on live posts.

## Standalone install (no link between sites)

Run every command in **PowerShell opened as Administrator** (right-click → *Run as
administrator*).

### 1. Get the files

1. Download this repository: on GitHub, **Code → Download ZIP**. Extract it to
   `C:\SiteControl-install`.
2. From the [latest release](../../releases/latest), download `sitectl.exe` and
   `SHA256SUMS.txt` into `C:\SiteControl-install`.
3. Check the download is intact:
   ```powershell
   cd C:\SiteControl-install
   Get-FileHash .\sitectl.exe -Algorithm SHA256
   Get-Content .\SHA256SUMS.txt
   ```
   The hash printed for `sitectl.exe` must match its line in `SHA256SUMS.txt`.

### 2. Describe the site

```powershell
Copy-Item deploy\examples\site-plan.standalone.example.toml site-plan.toml
notepad site-plan.toml
```

Change these settings (the comments in the file explain each one):

- `site_id`: a permanent short name, lowercase, **different at every site**
  (e.g. `iceroad`, `pad`, `npf`, `ndb`, `sid`).
- `display_name`: the name shown on every page.
- `db_path`: change the file name to match the `site_id`. Keep it under
  `C:\ProgramData\SiteControl\` and keep the **single quotes**.
- `site_type` and `orientation`.
- the `[[ungated]]` blocks: one per site that vehicles leave for. Set `at` to this
  post's `site_id`.
- `[post.road_work]`: keep it at an ice-road post, delete it everywhere else.

Save and close Notepad.

### 3. Generate the config

```powershell
.\sitectl.exe admin season -plan site-plan.toml -out C:\SiteControl-setup
```

Expected output: one line like `iceroad  iceroad\site.toml  accepts fp:…, 0 peer(s),
2 destination(s)`, then `wrote passwords.txt … and IT-CHECKLIST.txt`.
If it prints `REFUSED`, nothing was written. Fix the plan as the message says
and run it again (into a new or empty folder).

It wrote:

| File | What it holds |
|---|---|
| `C:\SiteControl-setup\<site_id>\site.toml` | The post's config, including its login password and key. **Secret.** |
| `C:\SiteControl-setup\passwords.txt` | The officers' login password. **Secret.** |
| `C:\SiteControl-setup\IT-CHECKLIST.txt` | Install checklist. No secrets |

### 4. Install the Windows service

```powershell
Set-ExecutionPolicy -Scope Process Bypass    # this window only; Windows blocks scripts by default
Get-ChildItem -Recurse | Unblock-File        # files downloaded from the internet are blocked until unblocked
.\deploy\windows\install-service.ps1 -SourceExe .\sitectl.exe -ConfigSource C:\SiteControl-setup\iceroad\site.toml
```

(Use your own `site_id` in place of `iceroad` in the last line.)

Expected: the output ends with `Service 'SiteControl' status: Running`.

The script:
- copies the program to `C:\Program Files\SiteControl\sitectl.exe`;
- copies the config to `C:\ProgramData\SiteControl\config.toml`;
- restricts that folder to Administrators, SYSTEM and the service;
- registers the `SiteControl` service to start automatically with Windows and
  restart if it ever stops unexpectedly;
- starts it.

### 5. Check it

Open <http://127.0.0.1:8081/status> in the browser. No login is needed. It should
say **"sitectl is running. Database self-created and answering."** A new
install's counts are all zero: 0 in, 0 out, 0 on site.

### 6. Hand over the password, then delete the setup folder

1. Open <http://127.0.0.1:8081>, enter the password from
   `C:\SiteControl-setup\passwords.txt`, and check that you can log in.
2. Give the password to the officers at that post.
3. Delete the setup folder. Apart from the installed config, it's the only copy of
   the password and key:
   ```powershell
   Remove-Item -Recurse -Force C:\SiteControl-setup
   ```

The password can still be read later by an administrator, in
`C:\ProgramData\SiteControl\config.toml` (`login_password`).

## After installing

- **Shortcut for officers:** put a browser shortcut to `http://127.0.0.1:8081` on
  the desktop.
- **Load the roster:** log in, then open <http://127.0.0.1:8081/roster/import>.
  The CSV columns are *last name, first name, phone, company*, and optionally
  *trade*. The page shows a preview and records nothing until you confirm. Or add
  people at the gate as they arrive.
- **Backups happen by themselves.** The first copy is taken about 2 minutes after
  the first start, then every night (around 03:00, or on waking if the laptop was
  asleep). The 7 newest copies are kept in `C:\ProgramData\SiteControl\backups\`.
  `/status` shows the newest copy. To also copy each backup to a USB drive, add
  `backup_mirror_dir = 'E:\SiteControl-backups'` near the top of `config.toml` and
  restart the service. Restoring is described in
  [`deploy/BACKUP-RESTORE.md`](deploy/BACKUP-RESTORE.md).
- **Protect the data:** the database holds names and phone numbers. Use full-disk
  encryption (BitLocker) and keep `C:\ProgramData\SiteControl` out of OneDrive.

## Linked install (sites share records)

Use this only once there is a working private link between the posts. The
differences from a standalone install:

1. Write **one** plan for **all** the posts: a `[[post]]` block each, with
   `listen_addr` and `base_url` set to the address each laptop has on the private
   link (e.g. `listen_addr = "10.8.0.11:8081"`, `base_url = "http://10.8.0.11:8081"`).
   Add a `[[route]]` (`a`, `b`, `travel_minutes`) for each road between two gated
   posts. Vehicle runs on these are timed.
   [`deploy/examples/site-plan.example.toml`](deploy/examples/site-plan.example.toml)
   shows the shape.
2. Run `admin season` **once** for the whole set. It generates matching keys for
   every pair of posts. Take each post's `site.toml` to its laptop and install it as
   in step 4 above.
3. On each laptop, allow the post's port inbound in Windows Defender Firewall,
   **only from the other posts' addresses** (listed in `IT-CHECKLIST.txt`):
   ```powershell
   New-NetFirewallRule -DisplayName "SiteControl" -Direction Inbound -Protocol TCP -LocalPort 8081 -RemoteAddress 10.8.0.12,10.8.0.13 -Action Allow
   ```
4. Check `/status` at each post. Once the others are running it shows
   `sync N peer(s) healthy`.

To add one post to a set that's already running, use `sitectl admin site add`
instead of `season`, so the new post's keys match the existing ones. See the
[Administrator Guide](docs/guides/SiteControl_Administrator_Guide.pdf),
"Creating New Sites", B.

## Updating, password changes, removal

- **Update the program:** download the new `sitectl.exe` into
  `C:\SiteControl-install`, then in an Administrator PowerShell:
  ```powershell
  Set-ExecutionPolicy -Scope Process Bypass
  .\deploy\windows\install-service.ps1 -SourceExe .\sitectl.exe
  ```
  With no `-ConfigSource`, the existing config and all the data are kept. Only the
  program is replaced.
- **Change the site password:** edit `login_password` in
  `C:\ProgramData\SiteControl\config.toml`, then run `Restart-Service SiteControl`.
  Tell the officers.
- **Remove:** `.\deploy\windows\uninstall-service.ps1` keeps the data.
  `.\deploy\windows\uninstall-service.ps1 -RemoveData` also deletes
  `C:\ProgramData\SiteControl`, **including the database and backups**.

## Troubleshooting

The Windows service keeps **no log file**. Use the `/status` page first. To see
the program's own messages, stop the service and run it by hand in an
Administrator PowerShell, then press Ctrl+C and start the service again:

```powershell
Stop-Service SiteControl
& 'C:\Program Files\SiteControl\sitectl.exe' -config C:\ProgramData\SiteControl\config.toml
Start-Service SiteControl
```

| Symptom | Cause and fix |
|---|---|
| `install-service.ps1 cannot be loaded … running scripts is disabled` | Run the `Set-ExecutionPolicy -Scope Process Bypass` line first, in the same window |
| `toml: … expected eight hexadecimal digits after '\U'` | A Windows path is in double quotes. Use single quotes: `db_path = 'C:\ProgramData\SiteControl\x.db'` |
| `unable to open database file` | The folder in `db_path` doesn't exist. The service installer creates `C:\ProgramData\SiteControl`; when running by hand, create it first |
| The page doesn't load, `/status` included | Check the service: `Get-Service SiteControl`. If another program uses port 8081, change the port in both `listen_addr` and `base_url` in `config.toml`, then restart the service |
| `REFUSED` from `admin season` | Nothing was written. The message names the setting to fix |
| "LOST SIGNAL" on a standalone post | It isn't standalone: the config lists other posts under `[[peers]]`. Regenerate it from a standalone plan |
