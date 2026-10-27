# Birrcare Configuration Guide

This guide explains how to configure Birrcare settings, manage household preferences, and safeguard family health records.

All settings live in the `.env` file at the root of the repository. Docker Compose reads `.env` automatically and passes the values to both containers.

---

## Managing Your Settings

### Automated Setup
When you run `./scripts/deploy.sh` or `./scripts/setup.sh`, the interactive assistant asks you a few simple questions in plain language and creates your `.env` file safely with restricted file permissions (`chmod 600`).

To reconfigure an individual setting later:
```bash
# Reconfigure a single value (e.g., doctor phone)
./scripts/setup-env.sh --reconfigure DOCTOR_PHONE

# Reconfigure all values interactively
./scripts/setup-env.sh --reconfigure

# Apply changes and restart containers
./scripts/deploy.sh restart
```

To run the interactive prompts in Arabic:
```bash
./scripts/setup-env.sh --lang ar
```

---

## Environment Variables Reference

### 1. Initial Admin Account (Database Seed)
These values are only used on the very first start, when the database is empty; `setup-env.sh` asks for them only then. Afterwards, accounts and PINs are managed in **Menu → User Management**.

| Variable | Default | Description |
|---|---|---|
| `SUPER_ADMIN_USER` | `admin` | Name of the first Super Admin account. |
| `SUPER_ADMIN_PIN` | asked | 4-digit PIN for that account, typed twice and hidden. `1234`, wrong lengths and repeated digits (`7777`) are refused. Once you've signed in, you can remove this line from `.env`. |

### 2. Household & Patient Care Settings

| Variable | Default | Description |
|---|---|---|
| `APP_TIMEZONE` | Detected | **Required in production.** The IANA time zone where the patient resides (e.g., `Asia/Riyadh`, `Asia/Bahrain`). This ensures scheduled doses, midnight cutoffs, and doctor reports align with local time. Phones read it from `/health`: "today", overdue doses and every time shown follow this zone, so a sibling helping from abroad sees home time, with a banner saying so when their phone's clock differs. |
| `EMERGENCY_NUMBER` | Suggested for your time zone | **Required.** The ambulance number for the crisis call button, e.g. `999` (Bahrain), `997` (Saudi Arabia); you confirm the suggestion during setup (`scripts/emergency-numbers`). It is also sent inside every page, so the button works offline; `112` is the last fallback. After changing it, `./scripts/deploy.sh restart`. |
| `DOCTOR_NAME` | Empty | Optional name of the treating neurologist or primary care physician. Displayed on crisis alerts. |
| `DOCTOR_PHONE` | Empty | Optional phone number for the attending physician. Enables a one-tap call button during crisis alerts and adverse drug events. |

### 3. Smart Caregiver Auth & Home Wi-Fi Switching

Birrcare lets family members switch profiles with one tap when caring for a parent at home, while requiring a PIN when away.

| Variable | Default | Description |
|---|---|---|
| `TRUSTED_HOME_IP` | Empty | Public IP address of the household internet connection (as seen by Cloudflare). Devices connecting from this IP can switch to caregiver profiles with 1 tap (admins always enter their PIN). Leave empty if your provider shares one public IP between homes (CGNAT). |
| `TRUSTED_HOME_SUBNETS` | Empty | Optional comma-separated CIDR ranges for home subnets (e.g., `192.168.1.0/24`). |

*Tip: Pairing a bedside tablet in **Menu → Home Devices** is even simpler and survives dynamic router IP changes.*

### 4. Cloudflare Zero Trust Access

Birrcare is designed to be hosted locally and accessed through a Cloudflare Tunnel with Cloudflare Access protection.

| Variable | Default | Description |
|---|---|---|
| `CF_ACCESS_TEAM_DOMAIN` | Empty | Your Cloudflare Zero Trust team domain (e.g., `yourteam.cloudflareaccess.com`). |
| `CF_ACCESS_AUD` | Empty | Application Audience (AUD) tag from Cloudflare Access. When both domain and AUD are set, the backend verifies signed JWTs and maps them to caregiver email addresses. |
| `CF_ACCESS_TRUST_EMAIL_HEADER` | `false` | **Always leave `false`.** Setting this to `true` trusts unverified raw headers and allows header spoofing. |

### 5. Lock-Screen Push Notifications (Web Push)

| Variable | Default | Description |
|---|---|---|
| `PUSH_CONTACT` | Project URL | Contact URI (`mailto:` or `https:`) sent to Apple and Google push notification gateways. |

### 6. Session & Security Controls

| Variable | Default | Description |
|---|---|---|
| `SESSION_SECRET` | Generated | Secret used to sign session cookies. `setup-env.sh` generates a 64-character one; changing it signs everyone out. Never shown on screen and never included in backups. |
| `DISABLE_2FA_FOR` | Empty | **Emergency Break-Glass Only:** If a Super Admin loses both their authenticator app and backup recovery codes, set this to their caregiver ID (`cg_superadmin`), restart once, remove it, and restart again. |

### 7. Network & Deployment Check

| Variable | Default | Description |
|---|---|---|
| `WEB_BIND` | `127.0.0.1` | Where the website listens. `127.0.0.1`: only this server (the Cloudflare connector runs here). `0.0.0.0`: also the local network, needed when the connector runs on another machine; otherwise Cloudflare shows *502 Bad gateway*. The backend always stays on `127.0.0.1`. After changing it, run `./scripts/deploy.sh restart`. |
| `PUBLIC_URL` | Empty | Public address of the app (e.g. `https://care.example.com`). After every deploy, `deploy.sh` checks that the new version answers there too, so a broken tunnel or DNS shows up at once. Used by the host scripts only; it isn't passed to the containers. |

### 8. Development & Testing Settings

| Variable | Default | Description |
|---|---|---|
| `AUTH_DEV_FALLBACK` | `false` | When `true`, unauthenticated requests act as the first active caregiver. **Never enable in production:** `setup-env.sh` refuses to continue while it is `true`. (Running the backend directly without `APP_ENV=production`, it is on unless you set it to `false`.) |
| `CORS_ALLOWED_ORIGINS` | Localhost | Comma-separated list of browser origins permitted to make cross-origin requests. Not needed when using the built-in Next.js proxy. |

---

## Container Environment Defaults

These variables are defined inside `docker-compose.yml` and pre-configured for the container network:

| Variable | Container | Value | Purpose |
|---|---|---|---|
| `PORT` | backend | `8080` | Internal HTTP listening port for Go backend. |
| `DB_PATH` | backend | `/data/birrcare.db` | Persistent SQLite database file location inside container. |
| `APP_ENV` | backend | `production` | Enables Secure cookie flags and disables development bypasses. |
| `INTERNAL_BACKEND_URL` | frontend | `http://birrcare-backend:8080` | Private Docker network URL where Next.js forwards `/api/v1/*`. |
| `NEXT_PUBLIC_API_URL` | frontend | `/api/v1` | Browser-side API endpoint prefix. |
| `NODE_ENV` | frontend | `production` | Optimizes Next.js runtime performance and asset delivery. |
| `EMERGENCY_NUMBER` | frontend | from `.env` | Sent inside every page as the offline fallback for the crisis call button. |
| `GIT_SHA` | both (build) | the commit | The version reported by `/api/v1/health`; set by `deploy.sh` or CI. |

The backend also keeps uploaded medicine photos in `data/photos/`, updater files in `data/update/` and backup files in `data/backup/`, next to the database (override with `PHOTO_DIR`, `UPDATE_DIR`, `BACKUP_DIR`).

---

## When an Update Needs a New Setting

Updates are installed from **Menu → Server** with nobody at the terminal, so they can't ask questions. Before you press Install, the server checks whether the new version needs a setting your `.env` doesn't have:
- If so, the app shows "This update needs a new setting: …" and Install is disabled.
- Run `./scripts/deploy.sh` on the server once to answer it, then install from the app.
- An install started anyway stops before rebuilding, and the running version stays up.

## Adding a New Setting (Developers)

Every setting the app reads must be described in [`scripts/env.schema`](../scripts/env.schema): one line `KEY|kind|required|default|validator|question (en)|question (ar)`. Add it to `.env.example` too, pass it to the container in `docker-compose.yml`, and read it by name with `os.Getenv("KEY")`. CI (`scripts/tests/env-schema.test.sh`) fails otherwise, so no setting ships without its question.
