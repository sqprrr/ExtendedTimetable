// htmx does not swap error responses; tell the user instead of silently
// doing nothing. One banner is reused, so several failed requests in a row
// show a single message rather than stacking dialogs.
let notice, noticeTimer;
function showNotice(msg) {
  if (!notice) {
    notice = document.createElement("div");
    notice.className = "notice";
    notice.setAttribute("role", "status");
    notice.setAttribute("aria-live", "polite");
    notice.addEventListener("click", () => notice.classList.remove("visible"));
    document.body.append(notice);
  }
  notice.textContent = msg;
  notice.classList.add("visible");
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => notice.classList.remove("visible"), 6000);
}
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
for (const form of document.querySelectorAll("form[data-autosubmit]")) {
  form.querySelector("button[type=submit]")?.setAttribute("hidden", "");
  form.addEventListener("change", () => form.requestSubmit());
  // Leave "all" choices out of the URL: ?subject_id=2, not ?subject_id=2&status=
  form.addEventListener("submit", () => {
    for (const s of form.querySelectorAll("select")) s.disabled = !s.value;
  });
}
// Back/forward can restore a page with the selects still disabled.
window.addEventListener("pageshow", () => {
  for (const s of document.querySelectorAll("form[data-autosubmit] select")) s.disabled = false;
});
