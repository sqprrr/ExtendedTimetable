// htmx does not swap error responses; tell the user instead of silently
// doing nothing. The layout holds one toast, so several failed requests in a
// row show a single message rather than stacking.
let toastTimer;
function showNotice(msg) {
  const toast = document.getElementById("toast");
  if (!toast) return;
  toast.querySelector("span").textContent = msg;
  toast.classList.add("visible");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove("visible"), 6000);
}
document.addEventListener("click", (e) => {
  if (e.target.closest("#toast")) e.target.closest("#toast").classList.remove("visible");
});
// Messages come translated from the page (data-msg-* on <body>).
const msg = (name) => document.body.dataset[name] || "";
document.addEventListener("htmx:responseError", (e) => {
  // Validation errors come back as a short plain-text message.
  const xhr = e.detail.xhr;
  const plain = (xhr.getResponseHeader("Content-Type") || "").startsWith("text/plain");
  const text = plain ? xhr.responseText.trim() : "";
  showNotice(text && text.length < 300 ? text : msg("msgSaveFailed"));
});
document.addEventListener("htmx:sendError", () => {
  showNotice(msg("msgOffline"));
});

// Ask for confirmation before submitting forms marked with data-confirm.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset && e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) e.preventDefault();
});

// Forms marked with data-autosubmit (the homework filters) apply a choice
// as soon as it changes; their button is only needed without JavaScript.
// Pages loaded in place by htmx bring new forms, so this runs on each.
function setUpAutosubmit(root) {
  for (const form of root.querySelectorAll("form[data-autosubmit]")) {
    if (form.dataset.ready) continue;
    form.dataset.ready = "1";
    form.querySelector("button[type=submit]")?.setAttribute("hidden", "");
    form.addEventListener("change", () => form.requestSubmit());
    // Leave "all" choices out of the URL: ?subject_id=2, not ?subject_id=2&status=
    form.addEventListener("submit", () => {
      for (const el of form.elements) {
        if (el.name && el.value === "" && (el.type !== "radio" || el.checked)) el.disabled = true;
      }
    });
  }
}
if (window.htmx) htmx.onLoad(setUpAutosubmit);
else setUpAutosubmit(document);
// Back/forward can restore a page with the fields still disabled.
window.addEventListener("pageshow", () => {
  for (const el of document.querySelectorAll("form[data-autosubmit] [name]")) el.disabled = false;
});

// Pages loaded in place (hx-boost; the skeleton shows meanwhile). The page
// is busy while it loads; an error page (404, 500) is a page too, so it is
// shown rather than reported; and focus moves to the new page's content.
const boosted = (e) => e.detail.boosted || e.detail.requestConfig?.boosted;
document.addEventListener("htmx:beforeRequest", (e) => {
  if (boosted(e)) document.getElementById("main")?.setAttribute("aria-busy", "true");
});
document.addEventListener("htmx:afterRequest", (e) => {
  if (boosted(e)) document.getElementById("main")?.removeAttribute("aria-busy");
});
document.addEventListener("htmx:beforeSwap", (e) => {
  if (e.detail.boosted && e.detail.xhr.status >= 400) {
    e.detail.shouldSwap = true;
    e.detail.isError = false;
  }
});
document.addEventListener("htmx:afterSettle", (e) => {
  if (e.detail.boosted) document.getElementById("main")?.focus({ preventScroll: true });
});

// The schedule's day strip selects a day in place: the circle moves to it,
// its card is marked (phones and the Day view show only that card) and
// scrolled to in the Week view, and the URL and the page's other links keep
// the choice. Without JavaScript the links load the page for that day.
document.addEventListener("click", (e) => {
  const day = e.target.closest?.(".day-strip a.day");
  if (!day || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  const key = day.dataset.day;
  const card = document.getElementById("day-" + key);
  if (!card) return;
  e.preventDefault();
  for (const a of day.parentElement.querySelectorAll("a.day")) {
    if (a === day) a.setAttribute("aria-current", "date");
    else a.removeAttribute("aria-current");
  }
  for (const c of document.querySelectorAll(".day-card.is-selected")) c.classList.remove("is-selected");
  card.classList.add("is-selected");
  history.replaceState(history.state, "", day.href);
  for (const a of document.querySelectorAll(".page-schedule a[href*='day=']:not(.day)")) {
    const url = new URL(a.href);
    url.searchParams.set("day", key);
    a.href = url.pathname + url.search;
  }
  for (const input of document.querySelectorAll(".page-schedule input[name=day]")) input.value = key;
  if (!card.closest(".view-day") && matchMedia("(min-width: 768px)").matches) {
    const smooth = !matchMedia("(prefers-reduced-motion: reduce)").matches;
    card.scrollIntoView({ block: "nearest", behavior: smooth ? "smooth" : "auto" });
  }
});

// Row menus are <details>: close the others when one opens, and all of
// them on a click elsewhere or Escape.
document.addEventListener("toggle", (e) => {
  if (!e.target.matches?.("details.menu") || !e.target.open) return;
  for (const d of document.querySelectorAll("details.menu[open]")) if (d !== e.target) d.open = false;
}, true);
document.addEventListener("click", (e) => {
  for (const d of document.querySelectorAll("details.menu[open]")) if (!d.contains(e.target)) d.open = false;
});
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  for (const d of document.querySelectorAll("details.menu[open]")) {
    d.open = false;
    d.querySelector("summary").focus();
  }
});

// Dates follow the device's time zone until the user picks one. The server
// cannot see the device's zone, so report it in a cookie (data-tz-cookie)
// whenever it is not the one the server last got (data-tz-device), and load
// the page again if it follows the device (data-tz-reload) and was drawn
// for a zone whose clock shows another time (data-tz-offset, minutes east
// of UTC). Only reload once the cookie has stuck: with cookies blocked, the
// page would otherwise reload forever.
(() => {
  const d = document.body.dataset;
  let tz = "";
  try {
    tz = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {}
  if (!d.tzCookie || !/^[A-Za-z0-9_+\-\/]{1,64}$/.test(tz) || tz === d.tzDevice) return;
  const secure = d.tzCookie.startsWith("__Host-") ? "; Secure" : "";
  document.cookie = `${d.tzCookie}=${tz}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`;
  const stuck = document.cookie.split("; ").includes(`${d.tzCookie}=${tz}`);
  const offset = -new Date().getTimezoneOffset();
  if (stuck && d.tzReload !== undefined && Number(d.tzOffset) !== offset) location.reload();
})();
