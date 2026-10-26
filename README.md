# Birrcare (رعاية بر)

Birrcare is a self-hosted, mobile-first web app designed to help siblings coordinate 24/7 home care for a parent recovering from a stroke.

Rooted in the Islamic value of *Birr al-Walidayn* (honoring and caring for one's parents), Birrcare gives the whole family a single, shared source of truth. When one sibling gives a medication dose, logs a blood pressure reading, or finishes a shift, every other family member's phone updates instantly without refreshing.

### 🎬 See it in action

https://github.com/user-attachments/assets/f0a95b11-2fde-4091-8391-a41bb261ec5e

*A narrated 1:57 tour of the main features: medication schedule, double-dose prevention, PRN safeguards, crisis alerts, the F.A.S.T. check, insulin barriers, shift handovers, the paramedic card, and Arabic/night mode. Screens use demo data.*

---

## Quick Start

You can get Birrcare running on your server in three simple commands:

```bash
git clone https://github.com/hussainalboori/BirrCare.git ~/birrcare
cd ~/birrcare
./scripts/setup.sh
```

The setup script checks your server, asks a few friendly questions (such as your household time zone and emergency number), and starts the app:

- **Frontend App:** [http://127.0.0.1:3000](http://127.0.0.1:3000)
- **Backend Health Check:** `curl http://127.0.0.1:8080/api/v1/health`

On first start, sign in with your configured Super Admin credentials. From there, you can add family members, set up medication schedules, and customize alert thresholds.

---

## Key Features

Birrcare is built specifically for real-world home caregiving, where multiple family members rotate shifts around the clock:

| Feature | How It Helps Your Family |
|---|---|
| **Double-Dose Prevention** | Doses are locked with two safety layers. A dose can never be recorded twice on the same schedule card, and a minimum interval lock stops repeat doses across cards. |
| **As-Needed (PRN) Safeguards** | For headache, pain, or fever relievers, the app tracks 24-hour rolling maximums and enforces minimum gaps between doses. |
| **Stroke Vitals & Crisis Alerts** | Logs blood pressure, pulse, oxygen saturation (SpO₂), temperature, and blood sugar. Hypertensive crises (≥180/120 mmHg) trigger an immediate red alert and one-tap emergency calling. |
| **Cognitive & F.A.S.T. Checks** | Tracks mental alertness (`alert`, `confused`, `restless`, `drowsy`). Signs of confusion lead to a rapid F.A.S.T. check that records the critical "last seen normal" timestamp for paramedics. |
| **Bedside Care & Repositioning** | A 2-hour repositioning timer helps prevent pressure ulcers (bedsores), alongside daily fluid intake and bowel care logs. |
| **Insulin Safety Barriers** | Displays the doctor's sliding scale, blocks repeat injections within the minimum interval, and strictly refuses correction insulin when blood sugar is below 3.9 mmol/L. |
| **One-Tap Shift Handovers** | Passing duty takes just a few taps with quick status chips. If a sibling does not accept the handover within 15 minutes, an alert is sent back to the sender. |
| **Emergency Paramedic Card** | A fridge QR code opens a lightweight summary for visiting paramedics without requiring a login. Critical blood thinners and recent vitals are highlighted at the top. |
| **Smart Switching & Tablets** | Family members at home can switch profiles with 1 tap. Bedside tablets can be put into "Shared Mode" with an automatic 20-minute idle logout. |
| **Sickroom Night Mode** | A true OLED black theme dims text and screens for quiet night shifts, keeping only emergency alerts brightly visible. |
| **Bilingual (Arabic & English)** | Native support for both Arabic and English with full Right-to-Left (RTL) and Left-to-Right (LTR) layout mirroring. |

---

## Centralized Logging and Audit Architecture

Birrcare implements a centralized, production-ready logging system that separates user activity from system events and strictly restricts log viewing to the Super Admin:

### 1. Separation of Log Streams
- **User Activity & Errors (`UserLogStore` / `user_logs`):** Captures caregiver actions, medication intake, vital entry, smart authentication switches, and user-facing HTTP errors (4xx/5xx).
- **System Events (`SystemLogStore` / `system_logs`):** Emits server lifecycle events, database migrations, connection pool state, Web Push deliveries, duty handover reminders, and backup/updater diagnostics.
- Both streams are captured through a single, concurrent-safe `Logger` instance that explicitly handles sink failures (outputting diagnostics to a fallback sink) rather than silently dropping events.

### 2. Role Model and Access Control
- **Super Admin Only (`super_admin`):** Access to view, search, paginate, or export logs across either stream is enforced server-side.
- **Strict Enforcement:** Unauthenticated requests receive `401 Unauthorized`. Requests from other authenticated roles (`admin`, `caregiver`, `viewer`) are rejected with `403 Forbidden`.
- Accessible via Super Admin endpoints under `/api/v1/admin/logs/user`, `/api/v1/admin/logs/system`, and `/api/v1/admin/logs/export`.

### 3. Auditable Log Access (`log_access_audits`)
- Every viewing, search, pagination, or export operation performed by a Super Admin is itself recorded into a dedicated access-audit store (`log_access_audits`).
- Access records capture the Super Admin ID, stream queried, operation type (`list`, `read`, `export`), applied filters, records count, IP address, and timestamp.
- The access audit records are themselves protected and viewable only by the Super Admin via `/api/v1/admin/logs/access-audits`.

---

## How It Works

Birrcare uses a lightweight, decoupled stack designed to run reliably on affordable hardware (such as an old laptop or a Raspberry Pi 4/5):

```
Caregiver Phones (iOS / Android) & Bedside Tablets
                       │
                       ▼ (HTTPS via Cloudflare Tunnel)
       ┌───────────────────────────────────────────────┐
       │ Host Machine: 127.0.0.1                       │
       │                                               │
       │   Next.js Frontend (:3000)                    │
       │   - Responsive App Router UI                  │
       │   - Bilingual RTL / LTR layout                │
       │   - Reverse proxy for /api/v1/*               │
       │          │                                    │
       │          ▼                                    │
       │   Go Backend (:8080)                          │
       │   - High-performance Chi HTTP router          │
       │   - In-memory Server-Sent Events broker       │
       │   - Pure Go SQLite (WAL Mode, Single Conn)    │
       │          │                                    │
       │          ▼                                    │
       │   Persistent Database: ./data/birrcare.db     │
       └───────────────────────────────────────────────┘
```

Both containers listen on `127.0.0.1` only. Public access is proxied through a secure Cloudflare Tunnel protected by Cloudflare Access, so the app is never exposed directly to the open internet.

---

## Making Your First API Call

If you are developing or testing locally, you can inspect the API directly using `curl`:

```bash
# 1. Fetch active caregivers for the switcher
curl -s http://localhost:8080/api/v1/caregivers

# 2. Authenticate with a 4-digit PIN (stores session cookie in cookies.txt)
curl -s -c cookies.txt -H "Content-Type: application/json" \
     -d '{"caregiver_id":"cg_superadmin","pin":"1234"}' \
     http://localhost:8080/api/v1/auth/verify-pin

# 3. View today's medication schedule and active doses
curl -s -b cookies.txt "http://localhost:8080/api/v1/medications/schedule"
```

---

## Documentation Guides

To keep this README easy to scan, in-depth technical references and operational guides live in dedicated documents:

| Guide | Audience | What It Covers |
|---|---|---|
| **[Production Deployment](docs/DEPLOYMENT.md)** | System Admins & Self-Hosters | Complete production setup, Cloudflare Tunnel & Access policies, automated updates, off-site encrypted backups, and rollbacks. |
| **[Configuration Reference](docs/CONFIGURATION.md)** | Admins & Maintainers | Full guide to all `.env` options, timezone settings, emergency contacts, home network detection, and security flags. |
| **[API Reference](docs/API.md)** | Developers & Integrators | Complete REST API endpoints, request and response schemas, error codes, and real-time Server-Sent Events (SSE). |
| **[Clinical Safety Guide](docs/CLINICAL.md)** | Caregivers & Clinical Reviewers | Blood pressure bands, pulse-ox and fever thresholds, hypoglycemia barriers, F.A.S.T. stroke protocol, and pressure ulcer prevention. |
| **[Developer Guide](docs/DEVELOPMENT.md)** | Software Contributors | Local environment setup (without Docker), running Go unit tests, Vitest, and Playwright E2E suites. |
| **[Frontend Web App](frontend/README.md)** | Frontend Engineers | Next.js App Router structure, Tailwind CSS 4 setup, and RTL/LTR localization. |

---

## Contributing

We welcome contributions from developers, designers, and healthcare professionals! Check out our **[Developer Guide](docs/DEVELOPMENT.md)** to learn about our testing standards, code conventions, and bilingual design system.

---

## License

This project is licensed under the **[Duaa License (رخصة دعاء)](LICENSE)** (based on the MIT License). 

As an expression of gratitude for using this software, you are kindly requested to recite Surah Al-Fatiha for the souls of the author's late father and two sisters, and make a sincere duaa for his mother's complete and swift recovery. See the [LICENSE](LICENSE) file for the full text.
