This is the Birrcare web app, a [Next.js](https://nextjs.org) project bootstrapped with [`create-next-app`](https://nextjs.org/docs/app/api-reference/cli/create-next-app). See the [root README](../README.md) for the whole system.

## Getting Started

The app calls the Go backend through its own `/api/v1` proxy (`src/app/api/v1/[...path]/route.ts`), which forwards to `INTERNAL_BACKEND_URL` (default `http://birrcare-backend:8080`, the Docker Compose service name). Start the backend first, then run the development server:

```bash
npm install
INTERNAL_BACKEND_URL=http://localhost:8080 npm run dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser to see the result.

You can start editing the page by modifying `src/app/page.tsx`. The page auto-updates as you edit the file.

This project uses [`next/font`](https://nextjs.org/docs/app/building-your-application/optimizing/fonts) to automatically optimize and load [Tajawal](https://fonts.google.com/specimen/Tajawal) (Arabic) and [Inter](https://fonts.google.com/specimen/Inter) (English).

## Tests

```bash
npm run lint
npm test           # Vitest unit tests (src/__tests__)
npm run test:e2e   # Playwright RTL/LTR checks; API calls are mocked, no backend needed
```

Before the first e2e run: `npx playwright install chromium webkit`.

## Learn More

To learn more about Next.js, take a look at the following resources:

- [Next.js Documentation](https://nextjs.org/docs) - learn about Next.js features and API.
- [Learn Next.js](https://nextjs.org/learn) - an interactive Next.js tutorial.

You can check out [the Next.js GitHub repository](https://github.com/vercel/next.js) - your feedback and contributions are welcome!

## Deployment

Birrcare is self-hosted with Docker Compose next to the Go backend, not on Vercel. See [Deployment](../README.md#deployment) in the root README.
