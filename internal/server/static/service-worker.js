self.addEventListener("push", (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch (_) {
    payload = {};
  }
  const target = new URL(payload.url || "/", self.location.origin);
  const url = target.origin === self.location.origin ? `${target.pathname}${target.search}${target.hash}` : "/";
  event.waitUntil(self.registration.showNotification(payload.title || "Track Slash", {
    body: payload.body || "Issue activity",
    data: { url },
    tag: payload.tag || undefined,
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || "/", self.location.origin);
  const url = target.origin === self.location.origin ? target.href : self.location.origin;
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of windows) {
      if ("navigate" in client) await client.navigate(url);
      if ("focus" in client) return client.focus();
    }
    return self.clients.openWindow(url);
  })());
});
