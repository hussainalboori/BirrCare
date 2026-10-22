# Birrcare Frontend Web Application

Welcome to the frontend application for **Birrcare (رعاية بر)**! This is a mobile-first, responsive web application built with **Next.js 16**, **React 19**, and **Tailwind CSS 4**.

It is designed to feel fast and comfortable on mobile phones (iOS Safari and Android Chrome) and wall-mounted bedside tablets, with native bidirectional support for Arabic (Right-to-Left) and English (Left-to-Right).

---

## How It Works

The frontend serves as both the client user interface and the local gateway for the system:

- **Same-Origin Security:** The browser only connects to port `3000`. The Next.js API route (`src/app/api/v1/[...path]/route.ts`) proxies `/api/v1/*` calls to the Go backend (`INTERNAL_BACKEND_URL`). This ensures session cookies remain strictly same-origin.
- **Zero-Refresh Sync:** The custom `useLiveSync()` hook maintains an open Server-Sent Events (SSE) connection. When any sibling logs a dose or vital on their phone, every other open screen updates in real time.
- **Offline Tolerance:** When a caregiver taps a dose while Wi-Fi is temporarily interrupted, the tap is stored locally on the device and sent once the connection is restored.
- **Night Mode:** A true OLED black theme (`Menu → Screen colours`) dims colors and text for quiet night shifts in the sickroom, keeping only critical alerts brightly colored.

---

## Getting Started Locally

To run the frontend on your local development machine:

### 1. Install Dependencies
```bash
npm install
```

### 2. Start the Dev Server
Point `INTERNAL_BACKEND_URL` to your running Go backend:
```bash
INTERNAL_BACKEND_URL=http://localhost:8080 npm run dev
```
Open [http://localhost:3000](http://localhost:3000) in your browser.

---

## Available Scripts

| Command | Description |
|---|---|
| `npm run dev` | Starts the Next.js development server with hot-module reloading. |
| `npm run build` | Builds an optimized production bundle. |
| `npm run start` | Runs the compiled production server. |
| `npm run lint` | Runs ESLint to check for code quality and syntax issues. |
| `npm test` | Runs unit tests using Vitest (in `src/__tests__/`). |
| `npm run test:e2e` | Runs mobile end-to-end tests using Playwright (API calls are mocked). |

Before your first Playwright run, make sure browser binaries are installed:
```bash
npx playwright install chromium webkit
```

---

## Typography & Bilingual Support

Fonts are bundled in `public/fonts/` and loaded with `@font-face` (`src/app/fonts.css`), so builds and phones never fetch them from Google:
- **Arabic:** [Tajawal](https://fonts.google.com/specimen/Tajawal) (`dir="rtl"`)
- **English:** [Inter](https://fonts.google.com/specimen/Inter) (`dir="ltr"`)

When writing styles, use Tailwind logical properties (`ms-*`, `me-*`, `start-*`, `end-*`) rather than physical directions (`ml-*`, `mr-*`, `left-*`, `right-*`) so layouts flip correctly between languages.

---

## Related Documentation

- [Root System README](../README.md): High-level project overview and quick start.
- [Developer Guide](../docs/DEVELOPMENT.md): Full-stack developer instructions and architecture details.
- [API Reference](../docs/API.md): Complete list of REST endpoints and real-time SSE events.
- [Clinical Guide](../docs/CLINICAL.md): Stroke vitals thresholds, dose locking, and care protocols.
