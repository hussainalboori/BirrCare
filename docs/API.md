# Birrcare API Reference

Welcome to the Birrcare API guide! Whether you are building an integration, troubleshooting a client request, or exploring how the mobile app syncs with the server, this document walks you through every endpoint and event.

The backend base path for all client requests is `/api/v1`. 

---

## Quick Navigation

- [Authentication & Security](#authentication--security)
- [Real-Time Updates (SSE)](#real-time-updates-server-sent-events)
- [Public Endpoints](#public-endpoints)
- [Caregiver Account & Device Pairing](#caregiver-account--device-pairing)
- [User Management (Super Admin)](#user-management-super-admin)
- [Medications & Scheduled Doses](#medications--scheduled-doses)
- [PRN (As-Needed) Medications](#prn-as-needed-medications)
- [Medicine Photos](#medicine-photos)
- [Stroke Vitals & Clinical Settings](#stroke-vitals--clinical-settings)
- [Bedside Care (Repositioning, Fluids, Bowel)](#bedside-care-repositioning-fluids-bowel)
- [Insulin Tracking](#insulin-tracking)
- [Shift Coordination & Handover Notes](#shift-coordination--handover-notes)
- [Emergency Paramedic Card](#emergency-paramedic-card)
- [Alerts & Web Push Notifications](#alerts--web-push-notifications)
- [Doctor Consultation Summary Export](#doctor-consultation-summary-export)
- [Server Updates & Backup Administration](#server-updates--backup-administration)
- [Standard Error Codes](#standard-error-codes)

---

## Authentication & Security

Birrcare checks who is making a request in this order:

1. **Session Cookie (`birrcare_session`):** Set by `POST /api/v1/auth/verify-pin` or optimistic home profile switching. It is an `HttpOnly`, `SameSite=Strict` cookie valid for 30 days on personal devices, or 20 minutes of idle time on shared tablets.
2. **Cloudflare Access JWT:** Reads `Cf-Access-Jwt-Assertion` header or `CF_Authorization` cookie, verified against your Cloudflare Zero Trust team domain and mapped to `caregivers.email`.
3. **Local Dev Fallback:** Active only when `APP_ENV` is not `production` and `AUTH_DEV_FALLBACK=true`. Never use this in production.

Every API response carries hardened security headers: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and `Cache-Control: no-store`.

### Caregiver Roles

| Role | Permissions |
|---|---|
| `super_admin` | Full control over users, role assignment, shifts, two-factor authentication, server updates, and off-site backups. |
| `admin` | Manages medications, schedules, insulin plans, clinical settings, and patient profile; can override interval locks and cancel doses with a logged reason. |
| `caregiver` | Logs daily doses, vitals, insulin, handovers, and bedside care events; acknowledges family alerts. |
| `viewer` | Read-only access to schedules, vitals, care overviews, and reports. |

---

## Real-Time Updates (Server-Sent Events)

### `GET /api/v1/events/stream`
- **Access:** Any authenticated caregiver
- **Description:** An open HTTP connection streaming Server-Sent Events (SSE). When any sibling records a dose, logs a vital, or passes duty, every open device receives an event instantly.

#### Protocol Flow:
1. Connect with headers: `Accept: text/event-stream`.
2. The server answers with an initial connection ping:
   ```
   event: connected
   data: {"status":"connected"}
   ```
3. A `: ping` heartbeat comment is sent every 15 seconds to keep the connection alive.
4. Data changes stream as `message` events:
   ```
   id: alt_12345678
   event: message
   data: {
     "id": "evt_98765432",
     "type": "MED_LOGGED",
     "timestamp": "2026-10-22T14:30:00Z",
     "actor": {
       "id": "cg_hussain",
       "name": "Hussain",
       "avatar_color": "#2563eb"
     },
     "payload": {
       "medication_id": "med_clopidogrel",
       "status": "taken"
     }
   }
   ```

#### Broadcast Event Types:

| Event Type | Triggered When |
|---|---|
| `MED_LOGGED` | A scheduled dose was recorded. |
| `MED_VOIDED` | A recorded dose was cancelled/voided. |
| `MED_EVENT` | A follow-up event was added (e.g., patient vomited 15 min after). |
| `MEDICATIONS_UPDATED` | Medication catalogue, dosing slots, or stock levels changed. |
| `PRN_LOGGED`, `PRN_VOIDED` | An as-needed PRN dose was recorded or voided. |
| `VITAL_LOGGED` | Blood pressure, pulse, SpO₂, temperature, or blood sugar was saved. |
| `VITAL_CORRECTED` | An earlier vitals reading was superseded by a corrected record. |
| `CARE_EVENT_LOGGED` | Bedside care was recorded (repositioning, fluid, bowel movement). |
| `INSULIN_LOGGED`, `INSULIN_PLANS_UPDATED` | An insulin dose was recorded or a plan was modified. |
| `NOTE_ADDED` | A shift handover note was saved. |
| `CAREGIVER_SWITCHED`, `CAREGIVERS_UPDATED` | A profile was switched or caregiver user accounts were updated. |
| `SHIFT_UPDATED`, `DUTY_UPDATED` | Roster assignments or active duty status changed. |
| `DUTY_PASSED`, `DUTY_ACCEPTED`, `DUTY_REMINDER` | Duty was passed to a sibling, accepted, or is still not accepted after 15 minutes. |
| `STOCK_UPDATED` | A medicine's stock count changed (low-stock warnings). |
| `CARE_OVERDUE` | Repositioning is overdue (sent to the caregiver on duty). |
| `EMERGENCY_CARD_OPENED` | Someone opened the paramedics' emergency card. |
| `UPDATE_AVAILABLE`, `UPDATE_STARTING` | A new version is offered (Super Admin), or an install is starting (everyone sees a banner). |

Events that also create an alert (crisis BP, low BP, alertness, F.A.S.T., vomited dose, dose override…) carry the alert's id as the SSE `id:` and an `alert_id` in the payload, so a phone that missed them gets them again from `GET /api/v1/alerts/unacked`.

---

## Public Endpoints

These endpoints do not require an active session cookie:

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` or `/api/v1/health` | Health check. Returns status, service name, timezone, and git version. |
| `GET` | `/api/v1/caregivers` | Returns active caregivers (`id`, `name`, `avatar_color`) for the login switcher. |
| `POST` | `/api/v1/auth/verify-pin` | Verifies a caregiver's 4-digit PIN. Returns a session cookie or a 2FA challenge. |
| `POST` | `/api/v1/auth/verify-2fa` | Completes login for accounts with an authenticator app. |
| `POST` | `/api/v1/auth/logout` | Revokes the current session and clears the cookie. |
| `GET` | `/api/v1/contacts` | Returns emergency phone numbers and doctor contact info. |
| `GET` | `/api/v1/emergency/{token}` | Returns patient medical details for paramedics and emergency staff. |

---

## Caregiver Account & Device Pairing

Birrcare supports both 4-digit PIN entry and 1-tap profile switching on trusted home devices. One tap only ever reaches **caregiver** accounts; admins, super admins and viewers always enter their own PIN (issue #53).

### `GET /api/v1/auth/me`
Returns the signed-in caregiver's profile, whether the device is in shared mode, and remaining idle timeout minutes.

### `GET /api/v1/auth/trust-status`
Checks if the current client is trusted: a paired bedside device (its `HttpOnly` cookie) or the configured home IP (`TRUSTED_HOME_IP`). The phone's GPS position is never used: it can be faked.

### `POST /api/v1/auth/switch-caregiver`
Switches to a caregiver account with a single tap, without a PIN, on a trusted home device:
```json
{
  "caregiver_id": "cg_fatima"
}
```
*Returns `403 PIN_REQUIRED` if the device is not trusted or the target is not a caregiver account, and `429 TOO_MANY_REQUESTS` after 10 switches a minute from one device. Every one-tap switch is audited as `1TAP_SWITCH` with the trust reason, paired device and client IP.*

### `POST /api/v1/auth/device-mode`
Toggles between a personal phone (30-day session) and a shared household tablet (20-minute idle sign-out):
```json
{
  "shared": true
}
```

### `POST /api/v1/auth/touch`
Resets the 20-minute idle countdown on shared devices when the user taps or types.

### `POST /api/v1/auth/change-pin`
Updates your own 4-digit PIN. Requires `current_pin` and `new_pin`. Signs out your account on all other devices.

### `POST /api/v1/auth/sign-out-others`
Keeps the current device signed in and revokes all other active sessions for your user.

### `POST /api/v1/auth/pair-device`
Pairs a shared tablet (e.g., bedside iPad) so caregivers can use 1-tap switching on it for 1 year. Requires an admin PIN. The device token is set only as an `HttpOnly` cookie, never returned in the body:
```json
{
  "device_name": "Bedside iPad",
  "name": "Bedside iPad",
  "pin": "2468"
}
```

### `GET /api/v1/auth/paired-devices` & `DELETE /api/v1/auth/paired-devices/{id}`
Lists or revokes paired household devices (Admin only).

### `GET /api/v1/settings/home-location` & `PUT /api/v1/settings/home-location`
Reads or updates the stored home GPS coordinates and radius (meters). Admin only, both ways. They no longer grant 1-tap switching (issue #53).

### `POST /api/v1/auth/verify-admin-pin` (Admin)
Checks the signed-in admin's own PIN before a destructive action: `{"pin": "4829"}` → `{"status": "verified"}`, or `403 INCORRECT_PIN`. Rate-limited like the PIN keypad.

### `POST /api/v1/auth/2fa/setup`, `POST /api/v1/auth/2fa/enable`, `POST /api/v1/auth/2fa/disable`
Sets up, enables, or disables TOTP two-factor authentication for Super Admins. Enabling returns 8 one-time backup recovery codes.

---

## User Management (Super Admin)

All user management endpoints require the `super_admin` role.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/users` | Lists all users (active and inactive) with roles, emails, and phone numbers. |
| `POST` | `/api/v1/users` | Creates a new user with name, role, email, phone, and initial 4-digit PIN. |
| `PUT` | `/api/v1/users/{id}` | Updates profile, language preference, or role (signs user out everywhere if role changes). |
| `POST` | `/api/v1/users/{id}/reset-pin` | Resets a caregiver's PIN and revokes their existing sessions. |
| `DELETE` | `/api/v1/users/{id}` | Toggles user status between active and inactive. |
| `GET` | `/api/v1/audit/verify` | Validates audit trail integrity and row hashes. |

---

## Medications & Scheduled Doses

### `GET /api/v1/medications`
Lists all active medications in the catalogue.

### `POST /api/v1/medications` (Admin)
Creates a new scheduled medication.
```json
{
  "name_ar": "كلوبيدوجريل",
  "name_en": "Clopidogrel",
  "dosage": "75 mg",
  "instructions_ar": "حبة واحدة بعد الغداء",
  "instructions_en": "One tablet after lunch",
  "frequency_slots": ["afternoon"],
  "days_of_week": ["sun", "mon", "tue", "wed", "thu", "fri", "sat"],
  "slot_times": {"afternoon": "14:00"},
  "is_critical": true,
  "remind": true,
  "active_ingredient": "clopidogrel",
  "crush": "ok",
  "split_ok": null,
  "with_food": "any",
  "drug_class": "antiplatelet",
  "min_hours_between": 18,
  "track_stock": true,
  "stock_units": 30,
  "units_per_dose": 1,
  "refill_threshold_days": 4
}
```

Optional `"photo_urls": [...]`: up to 6 photos uploaded to `/medications/photos` (box front, back, the pill…); repeats are dropped and the first becomes `image_url` (the main photo) unless one is given.

### `PUT /api/v1/medications/{id}` (Admin)
Edits a medicine (names, dosage, instructions, slots, days, times, photos, interval, PRN limits). Sending `photo_urls` replaces the list; `image_url` then picks the main photo (default: the first; none if the list is empty). The change is audited with the values before and after.

### `DELETE /api/v1/medications/{id}` (Admin)
Deactivates a medicine; its history stays. **Always requires the admin's own PIN** in the `X-Admin-PIN` header, wherever the device is (`403 ADMIN_PIN_REQUIRED` / `INCORRECT_ADMIN_PIN`).

### `PUT /api/v1/medications/{id}/schedule` (Admin)
Updates dosing slots, clock times, and days of week for a medication.

### `GET /api/v1/medications/schedule?date=YYYY-MM-DD`
Returns scheduled medication cards for a given day (defaults to today in household local time). Includes dose status, caregiver attribution, and interval locking countdowns.

**Dose by weekday (issue #62).** `POST /medications` and `PUT /medications/{id}` take `dose_by_day` (`{"sat": "2.5 mg", "sun": "2.5 mg"}`; `{}` clears it), kept in the schedule history like the other schedule fields. The schedule returns that day's dose as `medication.dosage`, with `dose_changed_from_yesterday`, and a logged dose's `dosage_snapshot` is that day's dose.

**Lab results (issue #62).** `POST /api/v1/labs` `{kind: "inr" | "hba1c" | "other", value, unit?, taken_on? (default today), next_test_on?, notes?, client_request_id?}` (caregivers and admins). An INR outside the doctor's range (`inr_target_low` / `inr_target_high` in `/settings/clinical`, default 2.0–3.0, allowed 1.5–4.5) is saved with `out_of_range: true` and raises an `inr_out_of_range` alert for each admin. `GET /api/v1/labs?kind=inr&days=180` → `{results, inr_target}`. The doctor report includes `lab_results` and `inr_target`.

**Crush, split and food (issue #64).** `POST /medications` and `PUT /medications/{id}` take `crush` (`ok`, `do_not_crush`, `ask_pharmacist`; default `ask_pharmacist`), `split_ok` (`true`, `false` or `null` = not known) and `with_food` (`with_food`, `empty_stomach`, `any`; default `any`). On an update, an absent field is unchanged. Other values answer `400 INVALID_CRUSH` / `INVALID_SPLIT_OK` / `INVALID_WITH_FOOD`. The fields are returned by `/medications`, by each `medication` in `/medications/schedule` and by the emergency card. `/medications/schedule` also returns `swallowing_difficulty: {at, by}` (or `null`) when a handover was tagged "swallowing difficulty" in the last 7 days; the emergency card returns `swallowing_difficulty: {at}` (no name).

**Same ingredient, two names (issue #61).** `POST /medications` and `PUT /medications/{id}` take `active_ingredient` (normalised to lowercase, e.g. `clopidogrel`) and `drug_class` (`antiplatelet`, `anticoagulant`, `insulin`, `antidiabetic`, `antihypertensive`, `statin`, `anticonvulsant`, `painkiller`, `other`). A second active medicine with the same ingredient (on create, an ingredient change, or re-activation) answers `409 DUPLICATE_INGREDIENT` with `existing: {id, name_ar, name_en, dosage}`; send `confirm_duplicate: true` with a `duplicate_reason` to save it anyway (audited as `DUPLICATE_INGREDIENT_CONFIRMED`). A second anticoagulant or antiplatelet saves with `warnings: ["two_blood_thinners"]`. The interval lock and PRN limits count doses across every medicine with the same ingredient.

**Schedule history (issue #57).** `PUT /api/v1/medications/{id}` and `PUT /api/v1/medications/{id}/schedule` take `effective_from: "today" | "tomorrow"` (default today): a change of slots, days, times or dosage applies from that day, and earlier days keep the schedule they had (`medication_schedule_versions`). Deactivating a medicine stops it from today; its past doses and gaps stay. `GET /medications/schedule?date=` and the doctor report use the schedule in effect on each day, include stopped medicines, and always show a dose that was logged. The report's `per_medication` rows carry `stopped_on` and `stopped_by` for a stopped medicine.

### `POST /api/v1/medications/log`
Records a scheduled dose.
```json
{
  "medication_id": "med_123",
  "slot": "morning",
  "scheduled_date": "2026-10-22",
  "status": "taken",
  "notes": "Taken with water",
  "given_at": "2026-10-22T08:15:00Z"
}
```
- **Allowed statuses:** `taken`, `partial`, `refused`, `skipped`.
- **`given_by`** (optional): the caregiver who actually gave it, when someone else records it ("Someone else gave it"). The entry is theirs; who tapped is kept as `recorded_by` (they can still undo within 10 minutes), and the audit row says it was recorded on their behalf. Must be an active caregiver who can give medicines, else `400 INVALID_GIVEN_BY`.
- **Overdue doses (issue #55):** a server watcher checks every minute. A scheduled dose with nothing logged gets a `dose_overdue` alert for the caregiver on duty (everyone if nobody is assigned) 45 minutes after its time, and the same alert goes to everyone at 90 minutes. Critical medicines (`is_critical`) use 30 and 60 minutes. One alert per dose; logging it in any way resolves it for everyone and broadcasts `ALERT_RESOLVED`; voiding that entry reopens it. No alert while the interval lock would refuse the dose, for medicines with `remind: false`, or for doses more than 12 hours old.
- **Response codes:**
  - `201 Created`: Dose successfully recorded.
  - `200 OK`: Duplicate resend detected (`already_logged_by_you: true`).
  - `409 DOSE_ALREADY_LOGGED`: Another sibling already recorded this dose.
  - `409 DOSE_TOO_SOON`: Blocked by minimum interval between doses.
- **Admin Overrides:** Admins can pass `"override_interval": true` and `"override_reason": "Doctor order"` to record a dose within the lock window.

### `POST /api/v1/medications/log/{id}/void`
Cancels an accidental dose entry. The caregiver who logged it can void within 10 minutes without a reason; admins can void any time with a mandatory explanation.

### `POST /api/v1/medications/log/{id}/event`
Records an adverse follow-up event on a taken or partial dose:
```json
{
  "type": "vomited",
  "minutes_after": 20
}
```
*Immediately alerts all family members with a recommendation to call the doctor or pharmacist.*

### `GET /api/v1/medications/stock` & `POST /api/v1/medications/{id}/stock`
Tracks pill inventory, daily consumption rates, and days remaining. Caregivers can record box refills (`action: "refill"`) or exact manual counts (`action: "count"`).

---

## PRN (As-Needed) Medications

PRN medications are taken when needed (for pain, headache, fever) rather than on a fixed schedule.

### `GET /api/v1/medications/prn`
Returns active PRN medications with current safety status:
- Rolling 24-hour dose count.
- Maximum doses allowed in 24 hours (`prn_max_per_24h`).
- Minimum hours between doses (`prn_min_hours`).
- Lock state and countdown until the next dose is safe.

### `POST /api/v1/medications/prn/log`
Logs a PRN dose:
```json
{
  "medication_id": "med_paracetamol",
  "reason_tag": "headache",
  "notes": "Complained of mild temple pain"
}
```
*Allowed reason tags: `headache`, `pain`, `fever`, `other`.*

### `POST /api/v1/medications/prn/log/{id}/void`
Cancels a PRN log entry (allowed for the author within 10 minutes, or admin any time with a reason).

---

## Medicine Photos

### `POST /api/v1/medications/photos`
Uploads a pill or packaging photo directly from a phone camera (JPEG, PNG, or WebP up to 5 MB). Returns `{ "photo_url": "/api/v1/medications/photos/photo_xyz.jpg" }`.

### `GET /api/v1/medications/photos/{name}`
Serves the stored medicine photo.

---

## Stroke Vitals & Clinical Settings

### `GET /api/v1/vitals?limit=30`
Fetches recent vitals logs, latest first.

### `POST /api/v1/vitals`
Logs stroke vitals and clinical assessments:
```json
{
  "vital_type": "full",
  "systolic": 135,
  "diastolic": 85,
  "pulse": 72,
  "blood_sugar": 6.2,
  "spo2": 97,
  "temperature_c": 36.8,
  "alertness_level": "alert",
  "notes": "Resting comfortably after breakfast"
}
```
- **Stroke F.A.S.T. Assessment:** Send `vital_type: "fast"` with `fast_result: {"face": false, "arm": true, "speech": false}`.
- **Alert Triggers:** Blood pressure crisis (≥180/120 mmHg), low blood sugar (<3.9 mmol/L, `sugar_low`), low SpO₂ (≤88%), high fever (≥ `fever_high_c`, or ≥ `fever_c` with SpO₂ ≤ `spo2_warn`: `fever_high`), low blood pressure, confusion, drowsiness, or any positive F.A.S.T. indicator automatically notifies all siblings immediately. A fever (≥ `fever_c`, `fever`) or a very high sugar (≥13.9 mmol/L, `sugar_very_high`) goes to the caregiver on duty, or to everyone if nobody is assigned.
- An insulin log (`POST /api/v1/insulin/log`) with a blood sugar below 3.9 raises `sugar_low` too, usually with a held dose.

### `POST /api/v1/vitals/{id}/correct`
Supersedes a mistaken reading with a corrected entry. The original record is retained for medical audit integrity and linked via `correction_id`.

### `PUT /api/v1/vitals/{id}/last-normal`
Records the exact timestamp when a stroke patient was last seen symptom-free ("last seen normal"), essential for hospital stroke teams.

### `GET /api/v1/settings/clinical` & `PUT /api/v1/settings/clinical` (Admin)
Reads or customizes clinical thresholds:
- `bp_low_systolic` / `bp_low_diastolic` (defaults: 90 / 60 mmHg).
- `spo2_warn` / `spo2_critical` (defaults: 92% / 88%).
- `fever_c` (default: 38.0 °C, allowed 37.5–39.0).
- `fever_high_c` (default: 39.0 °C, allowed 38.5–40.0 and above `fever_c`).

---

## Bedside Care (Repositioning, Fluids, Bowel)

### Falls and seizures (issue #60)
- `POST /api/v1/care/incidents` (caregivers and admins): `{kind: "fall", hit_head, awake_normal, pain_or_bleeding, notes?, client_request_id?}` or `{kind: "seizure", duration_seconds, notes?, client_request_id?}`. Saved as a `care_events` row (`fall` / `seizure`) with an alert to everyone. `escalate` (a red alert, counted as an emergency for the crisis bar, backups and updates): a fall with a head knock, not awake and talking normally, or new pain / a limb that won't move / bleeding; a seizure of 5 minutes or more, or a second one within 24 hours. The response lists any active blood thinners for the dispatcher. A resend with the same `client_request_id` returns the first one.
- `GET /api/v1/care/incidents?hours=24` lists recent falls and seizures. The emergency card carries `last_incident` (last 7 days, no names or notes) and the doctor report `incidents`.

### Hospital stay / away (issue #59)
- `GET /api/v1/care/pause` → `{paused, pause: {id, reason, started_at, started_by_name, notes}}`.
- `POST /api/v1/care/pause` `{reason: "hospital" | "travel" | "other", notes}` (caregivers and admins): `201`, or `409 PAUSE_OPEN` if care is already paused. `POST /api/v1/care/resume`: `200`, or `409 NOT_PAUSED`. Both are audited and broadcast (`CARE_PAUSED`, `CARE_RESUMED`).
- While paused the overdue-dose and repositioning watchers stay quiet (and a dose due during the pause is never chased afterwards; the repositioning clock restarts at the return home), and low-stock alerts wait. Logging is still allowed. The doctor report counts doses due in a pause as `paused` (per medicine and `total_paused`), not in `expected`, and lists the `pauses` of the period.

Care tracking helps siblings prevent pressure sores (bedsores) and track nutrition.

### `GET /api/v1/care/status?kind={reposition|fluid|bowel}`
Returns the real-time status of a specific care dimension (e.g., current posture, minutes since last turn, whether repositioning is overdue).

### `GET /api/v1/care/overview`
Returns a unified summary of bedside care: current position, today's total fluid intake, and the last recorded bowel movement.

### `POST /api/v1/care/events`
Logs a bedside care event:
```json
{
  "kind": "reposition",
  "value": "right",
  "skin_condition": "ok",
  "notes": "Turned to right side with pillow support"
}
```
- **Reposition values:** `left`, `right`, `back`, `chair`. `skin_condition` can be `ok` or `red_area`.
- **Fluid values:** String containing volume in mL (e.g., `"200"`), with optional `"thickened": true` for dysphagia support.
- **Bowel values:** `"normal"`, `"constipated"`, `"diarrhea"`, `"incontinent"`.

### `POST /api/v1/care/check-overdue`
Evaluates whether a patient has been in the same position for over 2 hours and issues repositioning reminders.

---

## Bruises & Bleeding Signs

For a patient on a blood thinner. Bleeding signs alert every sibling (alert kind `bleeding`, push "Bleeding sign reported").

### `GET /api/v1/bruises?days=30`
The last 1–365 days (default 30), newest first.

### `POST /api/v1/bruises`
```json
{
  "body_part": "arm_left",
  "size": "medium",
  "change": "new",
  "bleeding_signs": ["gums"],
  "photo_url": "/api/v1/bruises/photos/<uuid>.jpg",
  "notes": "Bumped the door",
  "client_request_id": "…"
}
```
- **body_part:** `arm_left`, `arm_right`, `hand_left`, `hand_right`, `leg_left`, `leg_right`, `belly`, `chest`, `back`, `face_head`, `other`.
- **size:** `small` (coin), `medium` (palm), `large`. **change:** `new`, `same`, `bigger`, `smaller`.
- **bleeding_signs:** `gums`, `nose`, `urine`, `stool`, `vomit`, `head_knock`, `cut_wont_stop`.
- A body part or at least one bleeding sign is required (`EMPTY_REPORT`). Viewers get `403`.

### `POST /api/v1/bruises/photos` & `GET /api/v1/bruises/photos/{name}`
Upload the photo first (JPEG/PNG body, as for medicine photos); it is re-saved as a JPEG without phone metadata in `data/photos/bruises/` and served to signed-in caregivers only.

---

## Insulin Tracking

### `GET /api/v1/insulin/plans`, `POST /api/v1/insulin/plans` & `PUT /api/v1/insulin/plans/{id}` (Admin)
Creates and edits doctor-prescribed insulin plans: either a fixed dose or a sliding-scale correction based on blood sugar.

### `GET /api/v1/insulin/status`
Returns each plan's last injection time, dose given, and interval lock status.

### `POST /api/v1/insulin/log`
Records an insulin administration:
```json
{
  "plan_id": "plan_rapid_acting",
  "units": 4,
  "blood_sugar": 11.2,
  "status": "given",
  "notes": "Given before dinner"
}
```
*Enforces clinical safety: rejects correction doses when blood sugar is below 3.9 mmol/L (`HYPO_NO_INSULIN`) and blocks repeated injections inside the plan's minimum window (`INSULIN_TOO_SOON`).*

---

## Shift Coordination & Handover Notes

### `GET /api/v1/handovers` & `POST /api/v1/handovers`
`GET` returns the latest notes (`?limit=`, default 20), or with `?date=YYYY-MM-DD` every note of that household day (the Report page). Reports sent from the Report page are notes with `"shift_type": "report"`; any number can be sent per day.

Logs shift handover summaries with structured status tags:
```json
{
  "shift_type": "evening",
  "tags": ["ate_well", "slept_well", "repositioned"],
  "summary": "Calm evening, watched television, vitals stable.",
  "urgent_flag": false
}
```
*Tags like `pain` and `swallowing_difficulty` automatically set `urgent_flag: true`.*

### Shift roster (changes: Super Admin)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/shifts` | Lists the shift definitions (name, start and end time, colour). |
| `POST` | `/api/v1/shifts` | Creates a shift. |
| `PUT` / `DELETE` | `/api/v1/shifts/{id}` | Edits or removes a shift. |
| `POST` | `/api/v1/shifts/presets` | Replaces the shifts with a preset pattern: `4h`, `8h`, `12h` or `24h` shifts. |
| `GET` | `/api/v1/shifts/assignments?date=YYYY-MM-DD` | Who covers which shift on that day (default: today). |
| `POST` | `/api/v1/shifts/assignments` | Assigns a caregiver to a shift on a date. |
| `DELETE` | `/api/v1/shifts/assignments/{id}` | Removes an assignment. |

### `GET /api/v1/shifts/current-duty`
Identifies which sibling is on duty right now.

### `POST /api/v1/shifts/handover`
Transfers duty from the active caregiver to a sibling. Hands over the roster assignment and creates a direct notification.

### `POST /api/v1/shifts/handover/{id}/accept`
The recipient accepts the duty assignment. If not accepted within 15 minutes, an alert is sent back to the original caregiver.

---

## Emergency Paramedic Card

### `GET /api/v1/emergency/{token}` (Public)
Publicly accessible without login (configured with Cloudflare Access Bypass). Designed for emergency paramedics scanning a fridge QR code.
- Lists active medications (blood thinners highlighted). Each carries `last_dose: {at, status, hours_ago, vomited}` from its latest taken or partial dose that wasn't cancelled (issue #56).
- `last_insulin: {at, units, plan_name_ar, plan_name_en, blood_sugar}` (the last insulin actually given) and `prn_24h: [{name_ar, name_en, dosage, count, total_dose, last_at}]` (as-needed doses in the last 24 hours; `total_dose` only when the dosage is a plain amount such as `1 g`).
- The card page shows these first, the blood thinner's last dose in bold, with times in the household zone and their UTC offset, and its "as of" time.
- Shows the last 3 blood pressure readings, blood sugar, and alertness.
- Shows the last F.A.S.T. stroke assessment with "last seen normal" time.
- Displays emergency contacts and doctor telephone numbers.
- Contains **no** passwords, PINs, emails, or audit logs. Rate limited to 10 requests per minute per IP.

### `GET /api/v1/patient` & `PUT /api/v1/patient` (Admin)
Reads or updates patient emergency details (name, date of birth, blood group, allergies, hospital MRN).

### `GET /api/v1/patient/emergency-link` & `POST /api/v1/patient/emergency-link` (Admin)
Fetches the active emergency link token or generates a new 256-bit token (invalidating previous QR codes immediately if lost).

---

## Alerts & Web Push Notifications

### `GET /api/v1/alerts/unacked`
Returns pending unacknowledged alerts from the last 24 hours for the logged-in caregiver. Each has `emergency: true|false`: emergencies (`bp_crisis`, `bp_low`, `fast_positive`, `spo2_low`, `sugar_low`, `alertness`, `fever_high`; `models.EmergencyAlertKinds`, mirrored in `clinical/alert-kinds.json`) show the red crisis bar and also postpone backups and update restarts.

### `POST /api/v1/alerts/{id}/ack`
Marks an alert as acknowledged by the current caregiver.

### `GET /api/v1/push/key`
Returns the server's public VAPID key for Web Push subscription.

### `POST /api/v1/push/subscribe` & `POST /api/v1/push/unsubscribe`
Registers or removes a browser's `PushSubscription` for lock-screen push alerts.

### `POST /api/v1/push/test`
Sends a safe test notification to your registered phones after 10 seconds.

---

## Appointments & Questions for the Doctor (issue #63)

Viewers can read everything here; caregivers and admins can change it. Every write is audited.

### `GET /api/v1/appointments`
`{upcoming, recent}`. `upcoming`: scheduled appointments from 6 hours ago on, soonest first. `recent`: done, cancelled or passed appointments of the last 90 days, newest first (up to 30). Each appointment: `{id, kind: "doctor" | "lab" | "physio" | "other", title, starts_at (UTC), date, time (household YYYY-MM-DD / HH:MM), location, notes, assigned_caregiver_id, assigned_name, status: "scheduled" | "done" | "cancelled", created_by_name, created_at, updated_at}`.

### `POST /api/v1/appointments` & `PUT /api/v1/appointments/{id}`
`{kind, title, date, time, location?, notes?, assigned_caregiver_id?, client_request_id?}`. `date` and `time` are household time (`APP_TIMEZONE`). A new appointment can't be in the past (`INVALID_DATE`); the person taking her must be an active, non-viewer member (`INVALID_CAREGIVER`). Only a scheduled appointment can be changed (`409 NOT_SCHEDULED`).

### `POST /api/v1/appointments/{id}/status`
`{status: "done" | "cancelled" | "scheduled"}`. Done or cancelled clears its open reminders.

### `GET /api/v1/appointments/{id}/ics`
"Add to my calendar": an iCalendar (RFC 5545) file with the time in UTC, a 2-hour alarm and a stable `UID`, so re-importing an edited appointment updates the event.

**Reminders.** The server checks every minute. The one taking her (everyone if nobody is set) gets an `appointment_reminder` at 20:00 household time the evening before and 2 hours before; each is sent once per start time, so a rescheduled appointment is reminded again. A reminder whose time had already passed when the appointment was added is not sent. The evening before the `next_test_on` date of the latest lab result of each kind, everyone gets a `lab_test_due` reminder.

### `GET /api/v1/doctor-questions`
`{open, answered}`: open questions oldest first; answers of the last 180 days, newest first. Each: `{id, text, by, created_by, created_at, answered_at?, answered_by_name?, answer?, appointment_id?}`.

### `POST /api/v1/doctor-questions`
`{text (≤ 500 letters), client_request_id?}`.

### `POST /api/v1/doctor-questions/{id}/answer`
`{answer, appointment_id?}`: "Doctor said…". The question moves to the history (`409 NOT_OPEN` if already answered).

### `DELETE /api/v1/doctor-questions/{id}`
Removes an open question (whoever wrote it, or an admin). The row is kept for the audit log.

The doctor report (`/export/summary`) includes `open_questions` (all, shown first) and `answered_questions` (answered in the period).

## Doctor Consultation Summary Export

### `GET /api/v1/export/summary?days=7`
Generates a printable clinical report covering 7 to 90 days:
- Medication adherence rate vs. scheduled expectations.
- Doses taken, partial, vomited, refused, skipped, and unrecorded.
- Daily blood pressure, pulse, and blood sugar averages.
- Lowest oxygen level and highest temperature per day.
- Cognitive alertness distribution and recent clinical notes.

---

## Server Updates & Backup Administration

Super Admins can manage server maintenance directly from **Menu → Server**:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/admin/update/status` | Checks current version, new CI-passed commits, and database migration notes. |
| `POST` | `/api/v1/admin/update/install` | Triggers installation of the verified update. |
| `POST` | `/api/v1/admin/update/skip` | Dismisses the current update prompt. |
| `PUT` | `/api/v1/admin/update/policy` | Sets update mode: `"manual"` (install on click) or `"auto"` (install upon passing CI). |
| `GET` | `/api/v1/admin/backup/status` | Reports off-site restic backup status, storage destinations, and test restore health. |
| `POST` | `/api/v1/admin/backup/run` | Triggers an immediate off-site backup. |
| `PUT` | `/api/v1/admin/backup/schedule` | Sets the backup times (1–6 a day, household time), "wait while the app is in use", and retention (`history` or `latest`). |

`POST /api/v1/admin/update/install` answers `409` with `NEEDS_CONFIG` (the new version needs a setting the server's `.env` lacks), `ACTIVE_CRISIS` (an emergency alert from the last 30 minutes is unacknowledged; send `confirm_during_alert: true` to install anyway), `STALE_TARGET` (a newer version is on offer) or `IN_PROGRESS`.

**Host-only endpoints.** `POST /internal/backup/snapshot`, `/internal/backup/verify` and `/internal/backup/due` are used by `scripts/backup.sh` on the server itself. They live outside `/api/v1`, so neither the website proxy nor the tunnel forwards them; they accept only local callers without proxy headers and need the shared secret in `data/backup/secret`.

---

## Standard Error Codes

Birrcare returns errors in JSON format:
```json
{
  "error": "DOSE_ALREADY_LOGGED",
  "message": "This dose was already recorded by Fatima at 08:05."
}
```

### Common Error Codes:
- `UNAUTHENTICATED`: No valid session cookie found; sign in with your PIN.
- `FORBIDDEN`: Your role does not have permission for this action.
- `INCORRECT_PIN`: Wrong 4-digit PIN entered on keypad.
- `TOO_MANY_ATTEMPTS`: Device throttled after repeated incorrect PIN attempts.
- `DOSE_ALREADY_LOGGED`: Dose already recorded for this scheduled time card.
- `DOSE_TOO_SOON`: Blocked by minimum safety interval between doses.
- `HYPO_NO_INSULIN`: Correction insulin rejected because blood sugar is below 3.9 mmol/L.
- `PIN_REQUIRED`: 1-tap profile switch rejected because the device is not trusted or the target account always needs its PIN.
- `PRN_TOO_SOON` / `PRN_LIMIT`: an as-needed dose is inside its minimum gap, or the 24-hour maximum is reached.
- `INSULIN_TOO_SOON`: inside the insulin plan's minimum interval.
- `ALREADY_VOIDED`: the dose entry was already cancelled.
- `ADMIN_PIN_REQUIRED` / `INCORRECT_ADMIN_PIN`: a destructive admin action needs the admin's own PIN.
- `NEEDS_CONFIG`, `ACTIVE_CRISIS`, `STALE_TARGET`, `IN_PROGRESS`: see Server Updates above.

Errors use HTTP status codes consistently: `400` invalid input, `401` not signed in, `403` not allowed, `404` not found, `409` conflicts with the current state (locks, duplicates), `413` upload too large, `429` too many PIN attempts (with `Retry-After`).
