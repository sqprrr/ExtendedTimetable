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
document.addEventListener("htmx:responseError", (e) => {
  // Validation errors come back as a short plain-text message.
  const xhr = e.detail.xhr;
  const plain = (xhr.getResponseHeader("Content-Type") || "").startsWith("text/plain");
  const text = plain ? xhr.responseText.trim() : "";
  showNotice(text && text.length < 300 ? text : "Could not save. Reload the page and try again.");
});
document.addEventListener("htmx:sendError", () => {
  showNotice("Could not reach the server. Check your connection and try again.");
});

// Ask for confirmation before submitting forms marked with data-confirm.
document.addEventListener("submit", (e) => {
  const msg = e.target.dataset && e.target.dataset.confirm;
  if (msg && !window.confirm(msg)) e.preventDefault();
});
