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
