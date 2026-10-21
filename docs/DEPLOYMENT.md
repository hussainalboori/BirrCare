# Deploying Birrcare (رعاية بر)

How to run Birrcare on your own server for the family: first install, Cloudflare Tunnel and Access, automatic updates, backups, restore and rollback.

Birrcare holds a parent's medication and health records. Keep it private: it should only be reachable through Cloudflare Access, never directly from the internet.

## What you end up with

```
Phone ──HTTPS──► Cloudflare Access (email login) ──► Cloudflare Tunnel (cloudflared on the host)
                                                          │
                                                          ▼
                                    127.0.0.1:3000  frontend (Next.js) ── /api/v1/* proxy ──►
                                    127.0.0.1:8080  backend (Go) ──► ./data/birrcare.db (SQLite)
```

- Both containers listen on `127.0.0.1` only. Nothing is reachable from the LAN or the internet except through the tunnel.
- The browser talks only to the frontend (port 3000). The frontend forwards `/api/v1/*`, including the live-update stream, to the backend, so the tunnel needs to expose **port 3000 only**.
- All data lives in one file on the host: `./data/birrcare.db` (plus its `-wal` / `-shm` companions while running).

## 1. Requirements

On the host (any 64-bit Linux, e.g. Ubuntu 24.04, Debian 12 or Raspberry Pi OS on a Pi 4/5):

| Need | Why |
|---|---|
| Docker Engine with the Compose plugin (`docker compose`) | Builds and runs both services. |
| The deploying user in the `docker` group | So the scripts and the systemd timer can run Docker without `sudo`. |
| `git`, `curl`, `jq` | Used by `scripts/auto-deploy.sh`. |
| `sqlite3` | **Install it.** `deploy.sh` uses `sqlite3 .backup` for a consistent snapshot of the live database. Without it the script falls back to `cp`, which can copy a half-written database while the backend is running. |
| `cloudflared` | The Cloudflare Tunnel connector. |
| ~2 GB free disk | Docker images and build cache, plus the database and backups. |
| Outbound HTTPS from the host | Lock-screen alerts are sent to Apple's and Google's push services. |

```bash
# Debian/Ubuntu example
sudo apt install -y git curl jq sqlite3
# Docker: https://docs.docker.com/engine/install/  then:
sudo usermod -aG docker "$USER"   # log out and back in afterwards
```

## 2. Get the code

The automatic updater's systemd unit expects the checkout at **`~/birrcare`**:

```bash
git clone https://github.com/hussainalboori/BirrCare.git ~/birrcare
cd ~/birrcare
```

## 3. Configure `.env`

```bash
cp .env.example .env
chmod 600 .env            # it holds the session secret and the first PIN
```

Edit `.env`. The values that matter in production:

| Variable | Set it to |
|---|---|
| `APP_TIMEZONE` | The household's IANA zone, e.g. `Asia/Riyadh`. Decides which day a dose belongs to. Required in production; the backend refuses to start without it to prevent early-morning doses landing on the wrong day. |
| `SUPER_ADMIN_USER` / `SUPER_ADMIN_PIN` | Name and **non-default** 4-digit PIN for the first account. Only used when the database is empty. |
| `EMERGENCY_NUMBER` | Local emergency number (e.g. `997` in Saudi Arabia). Shown as a call button on a blood-pressure crisis. It is also built into the app as the fallback for when a phone can't reach the server, so `./scripts/deploy.sh` (which rebuilds) is needed after changing it. If it's empty, the button uses `112` and admins see a red "not configured" banner. |
| `DOCTOR_NAME`, `DOCTOR_PHONE` | The doctor's call button: on a BP crisis and on partial or vomited doses. Empty hides the button. |
| `CF_ACCESS_TEAM_DOMAIN`, `CF_ACCESS_AUD` | From step 6, if you want Cloudflare sign-in to map to caregivers by email. Leave empty to use PIN login only. |
| `SESSION_SECRET` | Optional. Leave empty and one is generated and stored in the database on first boot. If you set it, use `openssl rand -base64 48`. Changing it later signs everyone out. |

Leave these as they are in production:

| Variable | Must be | Why |
|---|---|---|
| `AUTH_DEV_FALLBACK` | `false` | `true` lets anyone in as the first caregiver. |
| `CF_ACCESS_TRUST_EMAIL_HEADER` | `false` | The raw email header can be spoofed; the signed JWT is verified instead. |
| `DISABLE_2FA_FOR` | empty | Break-glass only, see [Lost 2FA phone](#lost-2fa-phone-and-recovery-codes). |

The full list with defaults is in [`.env.example`](../.env.example) and the [README](../README.md#configuration).

## 4. First start

```bash
./scripts/deploy.sh
```

This builds both images, starts them, and waits until the backend health check and the frontend answer. Expected ending:

```
    ✔ Backend is healthy on http://127.0.0.1:8080
    ✔ Frontend is healthy on http://127.0.0.1:3000
 ✔ Birrcare successfully deployed and verified!
```

Check it yourself:

```bash
curl -s http://127.0.0.1:8080/api/v1/health
# {"status":"healthy","service":"birrcare-backend"}
docker compose ps
```

The database is created and migrated automatically on first start. On later starts, migrations run before the server accepts requests. The backend refuses to start if a safety check fails, such as the double-dose lock missing from the database; read `docker compose logs birrcare-backend` if it doesn't come up.

> **Logging in over plain HTTP:** with `APP_ENV=production` (set by `docker-compose.yml`) session cookies are `Secure`. Browsers accept them on `https://` and on `http://localhost` / `http://127.0.0.1`, but **not** on `http://<LAN-IP>:3000`. Use the tunnel's HTTPS address, or an SSH tunnel to the host for local testing (`ssh -L 3000:127.0.0.1:3000 host`, then open `http://localhost:3000`).

## 5. Cloudflare Tunnel

Create a tunnel that points your subdomain at the frontend:

```bash
cloudflared tunnel login
cloudflared tunnel create birrcare
cloudflared tunnel route dns birrcare care.example.com
```

`~/.cloudflared/config.yml` (or `/etc/cloudflared/config.yml` when installed as a service):

```yaml
tunnel: birrcare
credentials-file: /home/<you>/.cloudflared/<tunnel-id>.json
ingress:
  - hostname: care.example.com
    service: http://127.0.0.1:3000
  - service: http_status:404
```

```bash
sudo cloudflared service install   # runs the tunnel at boot
```

If you manage tunnels from the Zero Trust dashboard instead (Networks → Tunnels), add a public hostname `care.example.com` → `HTTP` → `127.0.0.1:3000`.

Only port 3000 needs exposing. Do **not** route a hostname to port 8080.

## 6. Cloudflare Access (who may open the app)

In Zero Trust → Access → Applications → **Add an application** → *Self-hosted*:

1. **Application domain:** `care.example.com`.
2. **Policy:** *Allow*, with **Include → Emails** listing each family member's email. Use the one-time PIN login method or your identity provider.
3. Save. Every visit now requires one of those emails before Cloudflare forwards anything to the tunnel.

Optional, to have Access sign family members straight in:

1. Copy the application's **Application Audience (AUD) Tag** (application → Overview) into `CF_ACCESS_AUD`.
2. Copy your **team domain** (Settings → Custom Pages, e.g. `yourteam.cloudflareaccess.com`) into `CF_ACCESS_TEAM_DOMAIN`.
3. In the app, give each caregiver the same **email** they use for Access (user management).
4. Run `./scripts/deploy.sh` again.

When both values are set, the backend verifies Cloudflare's signed token and matches it to a caregiver by email. The PIN switcher still works on shared devices.

### Emergency card for paramedics (Bypass for `/emergency/*` only)

The emergency card QR code (Menu → *Emergency card for paramedics*) must open on a stranger's phone without a login. Add a **second** Access application that covers just that path and bypasses login:

1. Zero Trust → Access → Applications → **Add an application** → *Self-hosted*.
2. **Application domain:** `care.example.com`, **Path:** `emergency/*`. Cloudflare applies the most specific path, so the rest of the site stays protected by the first application.
3. **Policy:** action **Bypass**, **Include → Everyone**.
4. Save, then check from a phone that is not logged in, on mobile data: scanning the QR code opens the card. Opening `https://care.example.com/` on the same phone must still ask for the Access login.

Only `/emergency/<token>` is exposed. That page is a single HTML file with no scripts or other files. The 64-character token in the QR code is the secret (256 bits). A wrong token gets a plain "not found". Requests are limited to 10 per minute per visitor IP. Every opening is audited, and the family gets an alert (at most one every 10 minutes). If a printed QR code is lost or shared, an admin taps **New QR code** and the old one stops working at once.

Do **not** bypass `/api/*`: the card page fetches its data server-side, so the API stays behind Access.

## 7. First login and hardening

Open `https://care.example.com`, pass Access, then in the app:

1. **Sign in** as the Super Admin with `SUPER_ADMIN_PIN`.
2. **Change the PIN:** menu → *تغيير الرمز / My PIN*. `1234` and repeated digits are refused.
3. **Turn on two-step login** for the Super Admin: menu → *المصادقة الثنائية / 2-step login*. Scan the QR code with an authenticator app and **store the 8 recovery codes somewhere safe**.
4. **Add the family:** menu → *المستخدمون / Users*. Each person gets a role (`admin`, `caregiver` or `viewer`) and their own PIN. Make one other trusted person a second `super_admin`, so a forgotten PIN never locks everyone out of user management.
5. **Add medications and the doctor's insulin plan** if used.
6. On each phone, open the site and choose *Add to Home Screen*.

7. **Turn on lock-screen alerts on every family phone.** Without them, a crisis only shows when the app is open.
   - **iPhone (iOS 16.4 or later):** open the site in Safari, tap Share → *Add to Home Screen*, open Birrcare **from the Home Screen icon**, then tap *تفعيل التنبيهات على هذا الهاتف / Turn on alerts on this phone* on the home page and allow notifications. iOS only delivers push to apps added to the Home Screen.
   - **Android (Chrome):** tap the same button and allow notifications.
   - **Test it:** right after turning alerts on, tap *إرسال تنبيه تجريبي / Send a test alert* and lock the phone. A harmless test notification arrives in about 10 seconds. Don't log a fake crisis to test: it would go into the health record and the doctor report.

Check the security headers on the public address once:

```bash
curl -sI https://care.example.com/ | grep -iE "content-security-policy|x-frame-options|x-content-type-options"
```

You should see a CSP with `frame-ancestors 'none'`, `X-Frame-Options: DENY` and `X-Content-Type-Options: nosniff`.

## 8. Updates: offered in the app, installed when you say so

`scripts/auto-deploy.sh` checks `main` every 5 minutes. When there is a new commit **whose CI run on GitHub passed**, it tells the app, and the Super Admin sees a dot on **Menu → Updates**. That screen shows:

- the version running now;
- the list of changes;
- an amber notice when the update changes the database.

Press **Install now** when the house is calm, or **Skip this version**. Everyone with the app open sees "Birrcare is updating" for about a minute. Taps made during the restart wait on the phone and are sent when it is back. `deploy.sh` takes a database backup before every install.

**Install now** refuses while a crisis, low blood pressure or F.A.S.T. alert from the last 30 minutes hasn't been acknowledged, unless you confirm.

Nothing installs by itself unless you switch on **Automatic updates** on the same screen. With it on, every CI-passed commit installs within 5 minutes, which is how it worked before.

It is pull-based on purpose. Nothing on GitHub can run code on your host; the host only runs what is on `main`, and only after CI succeeded. The app never deploys either: it writes a request into `data/update/`, and the host checks it. It installs only if the request names exactly the latest CI-passed `main`.

Install the systemd **user** units:

```bash
mkdir -p ~/.config/systemd/user
cp ~/birrcare/scripts/systemd/birrcare-deploy.{service,timer,path} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now birrcare-deploy.timer birrcare-deploy.path
sudo loginctl enable-linger "$USER"   # keep user timers running when you're logged out and after reboot
```

The **timer** checks for updates. The **path** unit starts the install within seconds of tapping Install, instead of at the next 5-minute check. The units expect the checkout at `~/birrcare`.

Check it:

```bash
DRY_RUN=1 ~/birrcare/scripts/auto-deploy.sh      # what it would do now
systemctl --user list-timers birrcare-deploy.timer
journalctl --user -u birrcare-deploy.service -n 50
cat ~/birrcare/data/update/available.json        # what the app is being offered
```

**Upgrading from automatic deploys:** the first run of the new script finds no `data/update/policy.json`, so updates become manual. To keep installing automatically, either:

- switch on **Automatic updates** in the app; or
- run `echo '{"mode":"auto"}' > ~/birrcare/data/update/policy.json`.

Copy the new `birrcare-deploy.path` unit as shown above. Without it, an install starts at the next timer tick, up to 5 minutes later.

Notes:

- **Remember which commit is live:** the updater keeps it in `~/.local/state/birrcare/deployed-sha`, and `GET /api/v1/health` reports the running `version`. To force a redeploy of the same commit, delete that file.
- **Files in `data/update/`:** the app and the host talk through these files:
  - `available.json`: what is offered;
  - `policy.json`: manual or automatic, and the skipped version;
  - `request.json`: an install request;
  - `result.json`: how the last install went.

  If the screen says "The update checker isn't running on the server", the host hasn't written `available.json` for 30 minutes. Re-run the `systemctl` command above.
- **Local edits are lost:** `auto-deploy.sh` runs `git reset --hard` on the checkout. `.env` and `data/` are git-ignored, so they are not touched. Don't edit tracked files on the host; change them in the repository instead.
- **GitHub API limit:** it reads CI results through GitHub's unauthenticated API, which allows 60 requests an hour. It asks once per new commit and remembers the answer.
- **Downtime:** each install restarts both containers, so the app is unavailable for about a minute. Open phones reconnect and refresh on their own.

### Manual updates instead

```bash
cd ~/birrcare
git pull
./scripts/deploy.sh
```

## 9. Backups

### Automatic snapshot on every deploy

`deploy.sh` copies `data/birrcare.db` to `data/backups/birrcare_backup_YYYYMMDD_HHMMSS.db` before rebuilding, and keeps the **last 10**.

### Daily backups and an off-host copy

Deploy snapshots only happen when there is an update. Add a daily backup and copy it **off the machine**, because a dead SD card or disk takes `data/backups/` with it:

```bash
crontab -e
# Daily at 03:30: consistent snapshot, keep 30 days
30 3 * * * cd ~/birrcare && mkdir -p data/backups/daily && sqlite3 data/birrcare.db ".backup 'data/backups/daily/birrcare_$(date +\%F).db'" && find data/backups/daily -name '*.db' -mtime +30 -delete
```

Then sync `data/backups/` somewhere else, such as another machine, an encrypted cloud folder or a USB drive. It contains health records, so keep the copy private and encrypted.

Never back up by copying `birrcare.db` alone while the backend runs: the newest changes may still be in `birrcare.db-wal`. Always use `sqlite3 … ".backup …"`.

## 10. Restore a backup

```bash
cd ~/birrcare
docker compose stop birrcare-backend
cp data/birrcare.db data/birrcare.db.before-restore          # keep the current state, just in case
cp data/backups/birrcare_backup_20261020_093000.db data/birrcare.db
rm -f data/birrcare.db-wal data/birrcare.db-shm              # stale WAL files belong to the old database
docker compose start birrcare-backend
curl -s http://127.0.0.1:8080/api/v1/health
```

Everything recorded after the backup was taken is lost, so tell the family which doses and readings to re-enter.

## 11. Roll back a bad update

Updates can change the database layout. Migrations only move forward: a table is rebuilt once and older code may not read the new layout. So roll back the **code and the database together**, using the snapshot `deploy.sh` took just before the bad update:

```bash
cd ~/birrcare
systemctl --user stop birrcare-deploy.timer birrcare-deploy.path   # pause the updater
git log --oneline -5                                  # pick the last good commit
git reset --hard <good-sha>
ls -t data/backups/ | head                            # the snapshot from just before the bad deploy
# restore that snapshot (section 10), then:
./scripts/deploy.sh
echo <good-sha> > ~/.local/state/birrcare/deployed-sha             # tell the updater what is live
systemctl --user start birrcare-deploy.timer birrcare-deploy.path
```

With manual updates (the default) the app will offer the bad commit again, because it is still the latest `main`. Press **Skip this version** in Menu → Updates; a fixed commit on `main` is offered as soon as its CI passes. If you switched on **Automatic updates**, switch it off first, or keep the timer stopped until `main` is fixed. Otherwise the updater installs the bad commit again.

## 12. Day-to-day operations

| Task | Command |
|---|---|
| Status | `docker compose ps` |
| Backend logs | `docker compose logs -f --tail=100 birrcare-backend` |
| Frontend logs | `docker compose logs -f --tail=100 birrcare-frontend` |
| Updater logs | `journalctl --user -u birrcare-deploy.service -n 50` |
| Restart | `docker compose restart` |
| Stop / start | `docker compose down` / `./scripts/deploy.sh` |
| Health | `curl -s http://127.0.0.1:8080/api/v1/health` |
| Free disk from old builds | `docker image prune -f && docker builder prune -f` |

## Troubleshooting

### The site asks for a PIN again and again, or the PIN is "accepted" but nothing happens

The session cookie isn't being kept. Open the app through `https://care.example.com` or `http://localhost:3000`, not a LAN IP (see the note in [step 4](#4-first-start)).

### "Too many attempts" on the keypad

A phone that has signed in before keeps its own attempt allowance, so the family's own phones are unaffected by someone else's guesses. A phone that has never signed in shares the allowance of all unknown devices. Wait out the lock: the keypad shows the minutes, at most 15. The counters are kept in memory, so `docker compose restart birrcare-backend` also clears them. Resetting the PIN does not.

### Lost 2FA phone and recovery codes

The Super Admin can't sign in. On the host:

```bash
echo 'DISABLE_2FA_FOR=cg_superadmin' >> .env   # the Super Admin's caregiver id
./scripts/deploy.sh                            # 2FA is cleared on this start
sed -i '/^DISABLE_2FA_FOR=/d' .env             # remove it again
./scripts/deploy.sh
```

Then sign in with the PIN and set up 2FA again. Leaving the variable set would clear 2FA on every restart.

### Forgotten Super Admin PIN

Another Super Admin can reset it under Users. There is no break-glass for a forgotten PIN (`SUPER_ADMIN_PIN` is only used when the database is empty), so **make a second family member Super Admin** right after the first login, or keep the PIN with the recovery codes.

### Doses appear on the wrong day

`APP_TIMEZONE` is missing or wrong in `.env`. Fix it and run `./scripts/deploy.sh`.

### Backend won't start: `SAFETY: …`

A database safety check failed, for example the double-dose lock index is missing. This is deliberate: the app refuses to run without its locks. Restore the latest backup (section 10). If it keeps happening, open an issue with the log line.

### Lock-screen alerts don't arrive

- **iPhone:** the app must be opened from the Home Screen icon, not a Safari tab. Then turn alerts on in that app, and check Settings → Notifications → Birrcare.
- **Android:** check the site's notification permission in Chrome.
- **Host:** the backend must reach the internet. `docker compose logs birrcare-backend | grep PUSH` shows rejected or failed pushes.
- **Nothing is lost if a push fails:** the alert still appears in the app, with the alarm, the next time it opens, until it is marked seen.

### Live updates don't arrive on other phones

Check that the tunnel points at port 3000, which proxies the live stream, and not at 8080. Phones reconnect by themselves after sleep or a network change.

## Checklist

- [ ] `.env` from `.env.example`, `chmod 600`, `APP_TIMEZONE` set, non-default `SUPER_ADMIN_PIN`
- [ ] `AUTH_DEV_FALLBACK=false`, `CF_ACCESS_TRUST_EMAIL_HEADER=false`, `DISABLE_2FA_FOR` empty
- [ ] `sqlite3` installed on the host
- [ ] Tunnel → `http://127.0.0.1:3000` only; Access policy lists the family's emails
- [ ] Super Admin PIN changed, 2FA on, recovery codes stored safely, a second Super Admin added
- [ ] `EMERGENCY_NUMBER` and `DOCTOR_PHONE` set and tested from a phone
- [ ] Emergency card filled in, QR printed and on the fridge or front door, Access **Bypass** for `/emergency/*` only, tested from a logged-out phone
- [ ] Lock-screen alerts turned on and tested on every family phone (iPhone: from the Home Screen app)
- [ ] Updater timer and path units enabled with linger (Menu → Updates shows no "checker isn't running" notice)
- [ ] Daily backup plus an off-host copy, and one test restore done
