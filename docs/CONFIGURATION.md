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
These values are only used on the very first boot when the database is empty. Once initialized, accounts and credentials are managed in **Menu → Users**.

| Variable | Default | Description |
|---|---|---|
| `SUPER_ADMIN_USER` | `admin` | Initial Super Admin username. |
| `SUPER_ADMIN_PIN` | `1234` | Initial 4-digit PIN for the Super Admin account. Change this immediately upon first login. |

### 2. Household & Patient Care Settings

| Variable | Default | Description |
|---|---|---|
| `APP_TIMEZONE` | Detected | **Required in production.** The IANA time zone where the patient resides (e.g., `Asia/Riyadh`, `Asia/Bahrain`). This ensures scheduled doses, midnight cutoffs, and doctor reports align with local time. |
| `EMERGENCY_NUMBER` | `@emergency-for-tz` | **Required.** The emergency ambulance number to call during a hypertensive crisis (e.g., `997`, `999`, `112`). Embedded in every page so it works even offline. |
| `DOCTOR_NAME` | Empty | Optional name of the treating neurologist or primary care physician. Displayed on crisis alerts. |
| `DOCTOR_PHONE` | Empty | Optional phone number for the attending physician. Enables a one-tap call button during crisis alerts and adverse drug events. |

### 3. Smart Caregiver Auth & Home Wi-Fi Switching

Birrcare lets family members switch profiles with one tap when caring for a parent at home, while requiring a PIN when away.

| Variable | Default | Description |
|---|---|---|
| `TRUSTED_HOME_IP` | Empty | Public IP address of the household internet connection (as seen by Cloudflare). Devices connecting from this IP can switch profiles with 1 tap. |
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
| `SESSION_SECRET` | Auto-generated | Cryptographic secret (at least 32 characters) used to sign session cookies. Generated automatically on first setup. Changing it signs everyone out. |
| `DISABLE_2FA_FOR` | Empty | **Emergency Break-Glass Only:** If a Super Admin loses both their authenticator app and backup recovery codes, set this to their caregiver ID (`cg_superadmin`), restart once, remove it, and restart again. |
| `PUBLIC_URL` | Empty | Public internet URL of your application (e.g., `https://care.example.com`). Used by `deploy.sh` to verify end-to-end tunnel connectivity after updates. |

### 7. Development & Testing Settings

| Variable | Default | Description |
|---|---|---|
| `AUTH_DEV_FALLBACK` | `false` | When `true`, unauthenticated requests automatically act as the first admin. **Never enable in production.** |
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
