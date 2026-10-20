/* Birrcare service worker: shows alert push notifications on a locked phone (issue #12).
 * The payload is generic on purpose ("Blood pressure alert — open Birrcare"): no
 * names or readings on the lock screen. Opening the app shows the details. */

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

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
