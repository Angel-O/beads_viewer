// head_init.js: the three bootstrap steps that used to be inline <script>
// blocks in index.html. They live in a file so the dashboard's Content
// Security Policy can drop 'unsafe-inline' for scripts: an injected
// <script> or on*= handler is then refused by the browser even if a future
// rendering bug reintroduces an XSS sink.
//
// Load order matters: this file is referenced right after vendor/tailwindcss.js
// (the same position the tailwind.config block had) and still inside <head>,
// so the dark-mode class is set before the first paint and tailwind.config is
// assigned on the runtime's global exactly as before.

// 1. Theme: default to dark mode when no preference is stored, before first
//    paint so the page does not flash light.
(function () {
  var stored = null;
  try {
    stored = localStorage.getItem('darkMode');
  } catch (err) {
    // Storage can be unavailable (privacy modes); fall through to dark.
  }
  if (stored === 'true' || stored === null) {
    document.documentElement.classList.add('dark');
  }
})();

// 2. Tailwind runtime configuration (vendor/tailwindcss.js is already loaded
//    and exposes the `tailwind` global this assigns to).
tailwind.config = {
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
      colors: {
        beads: {
          50: '#f0f9ff',
          100: '#e0f2fe',
          200: '#bae6fd',
          300: '#7dd3fc',
          400: '#38bdf8',
          500: '#0ea5e9',
          600: '#0284c7',
          700: '#0369a1',
          800: '#075985',
          900: '#0c4a6e',
        }
      }
    }
  }
};

// 3. Cross-origin isolation via service worker for GitHub Pages (sql.js needs
//    SharedArrayBuffer). Uses the controller check, not sessionStorage, to
//    prevent infinite reload loops; iOS Safari cannot enable COI via a service
//    worker and is handled by the degraded-mode branch.
(function () {
  if (typeof crossOriginIsolated !== 'undefined' && crossOriginIsolated) {
    console.log('[COI] Already cross-origin isolated');
    return;
  }
  if (!('serviceWorker' in navigator)) {
    console.log('[COI] Service workers not supported');
    return;
  }
  // A service worker already controls this page but we are still not
  // isolated: the browser does not support COI via SW. Do not reload again.
  if (navigator.serviceWorker.controller) {
    console.log('[COI] SW is controlling but not cross-origin isolated');
    console.log('[COI] Browser limitation (iOS Safari?). Using degraded mode.');
    return;
  }
  console.log('[COI] Registering service worker for cross-origin isolation...');
  navigator.serviceWorker.register('./coi-serviceworker.js')
    .then(function () {
      console.log('[COI] Service worker registered');
      return navigator.serviceWorker.ready;
    })
    .then(function () {
      console.log('[COI] Service worker ready, reloading...');
      // Give the SW time to claim clients before reloading.
      setTimeout(function () { window.location.reload(); }, 200);
    })
    .catch(function (err) {
      console.warn('[COI] Service worker registration failed:', err);
    });
})();
