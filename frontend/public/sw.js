// Self-destructing service worker.
//
// Elevon POS registers no service worker. This file exists because the
// store's host once served a different app that registered /sw.js with a
// Workbox precache; a browser that used it keeps serving that app's cached
// shell ("Not Found") instead of Elevon until the worker is replaced. When the
// browser checks /sw.js for an update it finds this file, installs it, and the
// new worker removes itself, drops every cache and reloads the open pages.
//
// Pinned by src/lib/legacyServiceWorker.test.ts. Keep it, and keep it tiny.
self.addEventListener('install', function () {
  self.skipWaiting()
})

self.addEventListener('activate', function (event) {
  event.waitUntil(
    (async function () {
      await self.clients.claim()
      const keys = await caches.keys()
      await Promise.all(keys.map(function (key) { return caches.delete(key) }))
      await self.registration.unregister()
      const clients = await self.clients.matchAll({ type: 'window' })
      await Promise.all(clients.map(function (client) { return client.navigate(client.url) }))
    })(),
  )
})
