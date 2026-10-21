/* Birrcare service worker: alert push notifications on a locked phone (issue #12) and
 * an offline app shell (issue #16).
 * The payload is generic on purpose ("Blood pressure alert — open Birrcare"): no
 * names or readings on the lock screen. Opening the app shows the details. */

self.addEventListener("install", () => self.skipWaiting());

// App shell for offline use (issue #16): pages, scripts, styles, fonts and images
// are fetched from the network first and the last good copy is kept, so the app
// still opens when the Wi-Fi drops. The API (/api/) is never cached: health data
// and dose state always come live, and unsent taps wait in the outbox instead.
const SHELL_CACHE = "birrcare-shell-v1";

self.addEventListener("activate", (event) =>
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith("birrcare-shell-") && k !== SHELL_CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
);

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/") || url.pathname === "/sw.js") return;
  // Emergency card (issue #19): one cached copy per card, whatever the query
  // (the app refreshes it with ?prefetch=1), so SOS works in airplane mode. If
  // the server is down, the last copy (with its "as of" time) beats an error.
  const emergency = url.pathname.startsWith("/emergency/");
  const key = emergency ? url.origin + url.pathname : req;
  event.respondWith(
    fetch(req)
      .then(async (res) => {
        if (res.ok && res.type === "basic") {
          const copy = res.clone();
          caches.open(SHELL_CACHE).then((c) => c.put(key, copy)).catch(() => {});
        } else if (emergency && res.status >= 500) {
          const cached = await caches.match(key);
          if (cached) return cached;
        }
        return res;
      })
      .catch(async () => {
        const cached = await caches.match(key);
        if (cached) return cached;
        if (req.mode === "navigate") return (await caches.match("/")) || Response.error();
        return Response.error();
      })
  );
});

self.addEventListener("push", (event) => {
  let msg = { title: "Birrcare", body: "Alert — open Birrcare", tag: "birrcare-alert", url: "/" };
  try {
    if (event.data) msg = { ...msg, ...event.data.json() };
  } catch (_) {
    // Keep the generic text if the payload can't be read.
  }
  event.waitUntil(
    self.registration.showNotification(msg.title, {
      body: msg.body,
      tag: msg.tag,
      renotify: true,
      requireInteraction: true,
      icon: "/icon.svg",
      badge: "/icon.svg",
      data: { url: msg.url || "/" },
    })
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = new URL((event.notification.data && event.notification.data.url) || "/", self.location.origin).href;
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((wins) => {
      for (const w of wins) {
        if (new URL(w.url).origin === self.location.origin) {
          w.navigate(url).catch(() => {});
          return w.focus();
        }
      }
      return self.clients.openWindow(url);
    })
  );
});
