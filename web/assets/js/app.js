// Shared by every page: dialogs, the confirm dialog, toasts, the offline banner, and the
// glue between htmx responses and them. Pages rendered on the server (Routes so far) need
// nothing else.
(function () {
    "use strict";

    var D = window.Distribyted = window.Distribyted || {};

    // D.confirm({title, body, confirmLabel, danger}) resolves true if the user confirms.
    D.confirm = function (opts) {
        opts = opts || {};
        var dialog = document.getElementById("confirm-dialog");
        if (!dialog || typeof dialog.showModal !== "function") {
            return Promise.resolve(window.confirm(opts.body || "Are you sure?"));
        }
        document.getElementById("confirm-title").textContent = opts.title || "Please confirm";
        document.getElementById("confirm-body").textContent = opts.body || "Are you sure?";
        var ok = document.getElementById("confirm-ok");
        ok.textContent = opts.confirmLabel || "Confirm";
        ok.classList.toggle("btn-danger", !!opts.danger);
        ok.classList.toggle("btn-primary", !opts.danger);

        dialog.returnValue = "";
        return new Promise(function (resolve) {
            dialog.addEventListener("close", function () {
                resolve(dialog.returnValue === "ok");
            }, { once: true });
            dialog.showModal();
        });
    };

    // toasts: a message in the corner, read out by screen readers, gone after a few seconds
    function toast(level, message) {
        var region = document.getElementById("toasts");
        if (!region) return;
        var t = document.createElement("div");
        t.className = "toast-msg toast-" + (level || "info");
        if (level === "error") t.setAttribute("role", "alert");
        var icon = document.createElement("i");
        icon.className = "mdi " + (level === "error" ? "mdi-alert-circle-outline" : "mdi-check-circle-outline");
        icon.setAttribute("aria-hidden", "true");
        var text = document.createElement("span");
        text.textContent = message;
        var close = document.createElement("button");
        close.type = "button";
        close.className = "btn-icon";
        close.setAttribute("aria-label", "Dismiss");
        close.innerHTML = '<i class="mdi mdi-close" aria-hidden="true"></i>';
        close.addEventListener("click", function () { t.remove(); });
        t.append(icon, text, close);
        region.appendChild(t);
        setTimeout(function () { t.remove(); }, level === "error" ? 8000 : 4000);
    }
    D.toast = toast;

    // the sidebar: a drawer on phones, which the toggler opens and the close button, the
    // overlay or Escape closes; on wider screens the toggler narrows it to icons
    var body = document.body;
    var toggler = document.getElementById("sidebar-toggler");
    var phone = window.matchMedia("(max-width: 767.98px)");
    var overlay = null;
    function drawer(open) {
        body.classList.toggle("sidebar-mobile-in", open);
        body.classList.toggle("sidebar-mobile-out", !open);
        if (toggler) toggler.setAttribute("aria-expanded", String(open));
        if (open && !overlay) {
            overlay = document.createElement("div");
            overlay.className = "mobile-sticky-body-overlay";
            overlay.addEventListener("click", function () { drawer(false); });
            body.prepend(overlay);
        } else if (!open && overlay) {
            overlay.remove();
            overlay = null;
        }
        body.style.overflow = open ? "hidden" : "";
        if (open) {
            var first = document.querySelector("#sidebar a, #sidebar button");
            if (first) first.focus();
        } else if (toggler && document.activeElement && document.activeElement.closest("#sidebar")) {
            toggler.focus();
        }
    }
    if (toggler) {
        toggler.addEventListener("click", function (e) {
            e.preventDefault();
            if (phone.matches) {
                drawer(!body.classList.contains("sidebar-mobile-in"));
                return;
            }
            var narrow = !body.classList.contains("sidebar-minified");
            body.classList.toggle("sidebar-minified", narrow);
            body.classList.toggle("sidebar-minified-out", !narrow);
            toggler.setAttribute("aria-expanded", String(!narrow));
        });
        if (!phone.matches) toggler.setAttribute("aria-expanded", "true");
    }
    document.addEventListener("click", function (e) {
        if (e.target.closest("[data-close-sidebar]")) drawer(false);
    });
    document.addEventListener("keydown", function (e) {
        if (e.key === "Escape" && body.classList.contains("sidebar-mobile-in")) drawer(false);
    });
    phone.addEventListener("change", function () {
        if (!phone.matches) drawer(false);
    });

    var offline = {
        show: function () {
            var b = document.getElementById("offline-banner");
            if (b) b.hidden = false;
        },
        hide: function () {
            var b = document.getElementById("offline-banner");
            if (b) b.hidden = true;
        }
    };

    // dialogs open and close from buttons with data-open-dialog="id" and data-close-dialog
    document.addEventListener("click", function (e) {
        var open = e.target.closest("[data-open-dialog]");
        if (open) {
            var d = document.getElementById(open.dataset.openDialog);
            if (d) d.showModal();
            return;
        }
        var close = e.target.closest("[data-close-dialog]");
        if (close) {
            var dlg = close.closest("dialog");
            if (dlg) dlg.close();
        }
    });

    // copy buttons: data-copy names the field whose value they copy
    document.addEventListener("click", function (e) {
        var btn = e.target.closest("[data-copy]");
        if (!btn) return;
        var field = document.getElementById(btn.dataset.copy);
        if (!field || !navigator.clipboard) {
            if (field) { field.focus(); field.select(); }
            return;
        }
        var label = btn.querySelector("span") || btn;
        navigator.clipboard.writeText(field.value).then(function () {
            label.textContent = "Copied";
            setTimeout(function () { label.textContent = "Copy"; }, 1500);
        }, function () {
            field.focus();
            field.select();
        });
    });

    if (!window.htmx) return;

    document.body.addEventListener("htmx:confirm", function (e) {
        if (!e.detail.question) return;
        e.preventDefault();
        var elt = e.detail.elt;
        D.confirm({
            title: elt.dataset.confirmTitle,
            body: e.detail.question,
            confirmLabel: elt.dataset.confirmLabel,
            danger: elt.classList.contains("btn-icon-danger")
        }).then(function (yes) {
            if (yes) e.detail.issueRequest(true);
        });
    });

    // events a response names in its HX-Trigger header
    document.body.addEventListener("toast", function (e) {
        toast(e.detail.level, e.detail.message);
    });
    document.body.addEventListener("open-dialog", function (e) {
        var d = document.getElementById(e.detail.value);
        if (d && !d.open) d.showModal();
    });
    document.body.addEventListener("close-dialog", function (e) {
        var d = document.getElementById(e.detail.value);
        if (d && d.open) d.close();
    });

    document.body.addEventListener("htmx:sendError", function () {
        offline.show();
    });
    document.body.addEventListener("htmx:afterRequest", function (e) {
        if (e.detail.xhr && e.detail.xhr.status) offline.hide();
    });
    document.body.addEventListener("htmx:responseError", function (e) {
        var xhr = e.detail.xhr;
        // 401 sends the page to the login form (HX-Redirect); a failed refresh shows the
        // offline banner rather than a toast every two seconds
        if (xhr.status === 401 || e.detail.requestConfig.verb === "get") return;
        var text = (xhr.responseText || "").trim();
        toast("error", text && text.length < 300 && text.charAt(0) !== "<" ? text : "Something went wrong (" + xhr.status + ").");
    });
})();
