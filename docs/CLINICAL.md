# Birrcare Clinical & Care Coordination Guide

*Birrcare (رعاية بر)* is designed to help siblings coordinate round-the-clock home care for a parent recovering from a stroke. 

When caring for a recovering parent, multiple caregivers enter data at different hours of the day and night. This guide outlines the clinical safety rules, vital sign thresholds, and care routines built into Birrcare to keep parents safe.

> **Important Medical Disclaimer:** Birrcare is an open-source coordination tool, not a medical diagnostic device. The default thresholds in this document reflect standard clinical guidelines for stroke recovery, but you should always consult and follow your parent's treating physician and neurologist.

---

## 1. Stroke Vitals Classification

Blood pressure, pulse, blood sugar, oxygen, and alertness are evaluated whenever a reading is logged. Both the Go backend and Next.js frontend share the same test cases (`clinical/bp-cases.json` and `clinical/vitals-bands.json`) to guarantee identical clinical status classifications across devices.

### Blood Pressure (BP) Bands

Blood pressure management is critical during stroke recovery. The app color-codes readings and provides immediate guidance:

| Category | Systolic / Diastolic | Visual Badge | In-App Guidance & Action |
|---|---|---|---|
| **Crisis** | ≥ 180 **or** ≥ 120 mmHg | 🔴 **Red (Blinking)** | **Call emergency ambulance immediately.** Shows one-tap calling buttons (`997` / `112`), sounds an alert on all siblings' phones, and instructs caregivers to keep the patient calm and seated. |
| **Stage 2 Hypertension** | ≥ 140 **or** ≥ 90 mmHg | 🟠 **Orange** | **Notable elevation.** Check if prescribed antihypertensives were taken on schedule. Have the parent rest in a quiet room for 15 minutes and re-measure. |
| **Low Blood Pressure** | < 90 **or** < 60 mmHg *(or doctor custom limit)* | 🔴 **Deep Red** | **Hypotension risk.** Lay the parent flat, elevate legs slightly, re-measure in 5 minutes, and call the doctor. |
| **Elevated** | 120–139 **or** 80–89 mmHg | 🟡 **Yellow** | **Slightly elevated.** Continue routine monitoring at the next scheduled time. |
| **Normal & Stable** | < 120 **and** < 80 mmHg | 🟢 **Green** | **Normal range.** Within safe recovery targets. |

*Conflict Rule:* If systolic is in Crisis (e.g., 185) but diastolic is low (e.g., 55), the reading is categorized as **Crisis**. Higher risk always takes precedence.

### Pulse Oximetry (SpO₂) & Temperature

- **Oxygen Saturation (SpO₂):**
  - **Normal:** > 92% (Green).
  - **Low:** ≤ 92% (Orange warning). Advise caregiver to re-check on a warm finger. If persistent, contact the treating physician.
  - **Critical:** ≤ 88% (Red flashing alert). Instruct caregiver to sit the patient upright, confirm the pulse-ox clip is placed correctly, and call emergency services.
- **Body Temperature:**
  - **Normal:** 35.0 °C to 37.9 °C (Green).
  - **Fever:** ≥ 38.0 °C (Orange warning). Post-stroke fevers increase metabolic brain stress. Check for cough, aspiration risk, or urinary symptoms, and inform the doctor.
  - **Hypothermia:** < 35.0 °C (Orange warning). Warm the patient with blankets and re-measure.

*Custom Limits:* Admins can adjust the household's BP low limits, SpO₂ alert triggers, and fever threshold in **Menu → Clinical Settings** (`/settings/clinical`).

---

## 2. Blood Sugar & Insulin Safety Rules

Tight glycemic control prevents secondary neuronal injury after stroke.

### Blood Sugar Reference Ranges (mmol/L)

| Level (mmol/L) | Status | Clinical Action |
|---|---|---|
| **< 3.9** | 🔴 **Severe Hypoglycemia** | **Emergency.** Provide fast-acting oral carbohydrates (juice, honey, sugar water) immediately if conscious. Re-check in 15 minutes. |
| **3.9 – 7.8** | 🟢 **Normal Target** | Safe range for stroke recovery. |
| **7.9 – 13.8** | 🟡 **Elevated** | Verify scheduled diabetes medications and encourage proper hydration. |
| **≥ 13.9** | 🔴 **Severe Hyperglycemia** | High glucose damages brain microvasculature. Check physician-ordered sliding scale or contact doctor. |

### Insulin Safety Locks
1. **Hypoglycemia Lock:** Birrcare strictly refuses to log sliding-scale correction insulin if the recorded blood sugar is below 3.9 mmol/L (`HYPO_NO_INSULIN`).
2. **Interval Lock:** The app tracks when the last injection was administered and prevents repeat injections inside the plan's minimum interval (`INSULIN_TOO_SOON`).
3. **No Calculation:** Birrcare displays the doctor's scale table but never calculates an unapproved dose.

---

## 3. Cognitive Alertness & F.A.S.T. Assessment

Mental status fluctuations are a primary warning sign of secondary stroke or cerebral edema.

### Alertness Taxonomy
- **`alert` (يقظ ومدرك):** Normal eye contact, clear awareness, responds appropriately to speech.
- **`confused` (مشوشة / بطيئة الاستجابة):** Disorientation, slowed processing, or difficulty following simple requests. Prompts an immediate F.A.S.T. check.
- **`restless` (مضطرب / قلق):** Motor agitation, fidgeting, or unexplained distress.
- **`drowsy` (صعب إيقاظها):** Difficult to awaken, abnormal lethargy, or stupor. Raises an alert to all siblings.

### F.A.S.T. Stroke Protocol (`/fast`)
When confusion or motor weakness is noticed, any caregiver can launch the rapid F.A.S.T. check:
- **Face:** Ask the parent to smile. Does one side of the face droop?
- **Arms:** Ask the parent to raise both arms. Does one arm drift downward?
- **Speech:** Ask the parent to repeat a simple sentence. Is speech slurred or strange?
- **Time / Last Seen Normal:** If any check is positive, the app displays **"Call emergency ambulance now"** and records the **"Last seen normal"** timestamp. This timestamp is vital for hospital teams to determine eligibility for clot-busting medication (tPA) or thrombectomy.

---

## 4. Medication Double-Dose Prevention

Siblings rotating shifts in a busy home can easily lose track of whether a dose was given. Birrcare applies two layers of protection:

### Lock 1: Same Card Lock (Single Active Dose per Slot)
A partial unique database index enforces that each medication slot on a given calendar day (`medication_id`, `scheduled_date`, `slot`) can only have one active dose record. If Sibling A marks the 08:00 morning dose, Sibling B's screen locks instantly, turning green and displaying:
- *AR:* `أُعطي بواسطة حسين في 08:05`
- *EN:* `Given by Hussain at 08:05`
Any simultaneous attempt by another device returns a `409 DOSE_ALREADY_LOGGED` error detailing who gave it and when.

### Lock 2: Minimum Interval Across Doses
Even across different schedule cards (such as yesterday's bedtime dose and today's early morning dose), medications enforce a minimum safety interval (e.g., 9 hours or ¾ of the shortest scheduled gap). Trying to record another dose too early returns `409 DOSE_TOO_SOON`. Admins can override this with a recorded medical reason (e.g., doctor instruction).

### Handling Partial & Vomited Doses
- **Partial or Refused:** Recorded honestly as `partial` or `refused` rather than guessing.
- **Vomited Medication:** If a parent vomits 15 minutes after taking pills, the caregiver adds a `vomited` event.
- **No Automatic Re-Dose:** The app **never** suggests re-dosing. Instead, it alerts the family with an explicit instruction to call the doctor or pharmacist to determine whether a replacement dose is safe.

### As-Needed (PRN) Medications
For pain, fever, or headache relief, PRN medicines enforce:
- Maximum doses allowed in any rolling 24-hour period (`prn_max_per_24h`).
- Minimum hours between consecutive doses (`prn_min_hours`).

---

## 5. Bedside Care: Repositioning, Fluids, and Bowel

### Repositioning Schedule (Pressure Ulcer Prevention)
Stroke patients with hemiplegia (paralysis) cannot shift weight independently and face high risks of pressure ulcers (bedsores):
- **2-Hour Clock:** The system tracks patient posture (`left side`, `right side`, `back`, `chair`).
- **Overdue Reminders:** If 2 hours elapse without repositioning, Birrcare notifies the active caregiver and highlights the Bedside Care card in amber.
- **Skin Condition Check:** Caregivers log whether the skin is `ok` or has a `red_area`, catching redness before skin breakdown occurs.

### Fluid & Hydration Tracking
- Post-stroke dysphagia (swallowing impairment) increases dehydration risks.
- Caregivers log fluid intake in mL.
- A **"Thickened" (مكثف)** toggle records whether swallowing thickening powder was added to reduce aspiration pneumonia risks.

### Bowel Care Monitoring
- Immobility and medications frequently cause severe constipation or impaction.
- Tracks daily bowel movements (`normal`, `constipated`, `diarrhea`).
- Alerts caregivers if several days pass without a bowel movement.

---

## 6. Emergency Paramedic Card (`/emergency/{token}`)

In a crisis, visiting ambulance paramedics need immediate medical facts without waiting for someone to unlock a phone or log into an app.

- **Fridge / Door QR Code:** A printed QR code opens a lightweight, standalone medical summary.
- **Paracetamol & Blood Thinners:** Anticoagulants and antiplatelet drugs (e.g., Clopidogrel, Aspirin, Warfarin) are prominently highlighted at the very top.
- **Recent Vitals:** Displays the last 3 blood pressure readings, blood sugar, and F.A.S.T. stroke checks with the "last seen normal" timestamp.
- **Privacy Guaranteed:** The emergency card displays only medical essentials and family emergency phone numbers. It contains no logins, passwords, audit logs, or financial data.
- **Family Notification:** Every time the QR code is opened, all family members receive an alert notification.
