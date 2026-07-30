Handlebars.registerHelper('ibytes', function (bytesSec, timePassed) {
    return Humanize.ibytes(bytesSec / timePassed, 1024);
});
Handlebars.registerHelper('bytes', function (bytes) {
    return Humanize.bytes(bytes, 1024);
});


var Distribyted = Distribyted || {};

// fetch() follows redirects transparently: a request to a protected /api/*
// route whose session has expired lands on the (200 OK) /login page, and
// response.ok is true for that final response. Without this check, callers
// would try to parse the login page's HTML as JSON/log lines and either
// silently show nothing or, worse, report a mutating request (delete,
// add magnet, save config) as successful when it never happened.
Distribyted.auth = {
    _redirecting: false,

    // Detects an expired-session redirect and re-authenticates once.
    handleResponse: function (response) {
        if (!response.redirected || response.url.indexOf('/login') === -1) {
            return false;
        }
        if (!this._redirecting) {
            this._redirecting = true;
            window.location.href = '/login?next=' + encodeURIComponent(window.location.pathname);
        }
        return true;
    }
};

Distribyted.offline = {
    _isOffline: false,
    _banner: null,

    _getBanner: function () {
        if (this._banner) return this._banner;
        var div = document.createElement('div');
        div.id = 'offline-banner';
        div.className = 'alert alert-danger m-3 mb-0';
        div.setAttribute('role', 'alert');
        div.style.display = 'none';
        div.innerHTML = '<i class="mdi mdi-wifi-off mr-2"></i><strong>Connection lost</strong> — Could not reach server. Retrying…';
        var wrapper = document.querySelector('.content-wrapper');
        if (wrapper) wrapper.prepend(div);
        this._banner = div;
        return div;
    },

    show: function () {
        if (this._isOffline) return;
        this._isOffline = true;
        this._getBanner().style.display = 'block';
    },

    hide: function () {
        if (!this._isOffline) return;
        this._isOffline = false;
        var b = document.getElementById('offline-banner');
        if (b) b.style.display = 'none';
    }
};

Distribyted.poller = function (fn, activeMs, hiddenMs) {
    var timer = null;

    function tick() {
        fn();
        schedule();
    }

    function schedule() {
        clearTimeout(timer);
        timer = setTimeout(tick, document.hidden ? hiddenMs : activeMs);
    }

    document.addEventListener("visibilitychange", function () {
        if (!document.hidden) {
            clearTimeout(timer);
            tick();
        }
    });

    tick();
};

// Distribyted.api wraps the fetch+auth-check+offline-banner+JSON-parse
// sequence that used to be copy-pasted in every page's JS file (dashboard.js,
// routes.js, links.js, servers.js). request() resolves with the parsed JSON
// body (or null for the empty-body 200s several handlers return, e.g.
// apiAddTorrentHandler) and rejects with a real Error carrying the server's
// {error: "..."} message when available.
Distribyted.api = {
    request: function (url, opts) {
        return fetch(url, opts)
            .then(function (response) {
                if (Distribyted.auth.handleResponse(response)) {
                    return Promise.reject(new Error('session expired'));
                }

                Distribyted.offline.hide();

                if (response.status === 204) return null;

                return response.text().then(function (text) {
                    var body = null;
                    if (text) {
                        try { body = JSON.parse(text); } catch (e) { body = null; }
                    }

                    if (!response.ok) {
                        var message = (body && body.error) ? body.error : ('Request failed: ' + response.status);
                        throw new Error(message);
                    }

                    return body;
                });
            })
            .catch(function (error) {
                if (error.message !== 'session expired') {
                    Distribyted.offline.show();
                }
                throw error;
            });
    },

    get: function (url) {
        return this.request(url);
    },

    post: function (url, body) {
        return this.request(url, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
    },

    del: function (url) {
        return this.request(url, { method: 'DELETE' });
    }
};

// Distribyted.template memoises fetching + compiling a Handlebars partial,
// replacing per-page duplication in routes.js, links.js, and servers.js.
Distribyted.template = function (name) {
    Distribyted._templates = Distribyted._templates || {};
    if (Distribyted._templates[name]) return Distribyted._templates[name];

    var compiled = fetch('/assets/templates/' + name + '.html')
        .then(function (response) {
            if (response.ok) return response.text();
            Distribyted.message.error('Error getting data from server. Response: ' + response.status);
        })
        .then(function (t) {
            return Handlebars.compile(t);
        })
        .catch(function (error) {
            Distribyted.message.error('Error getting ' + name + ' template: ' + error.message);
        });

    Distribyted._templates[name] = compiled;
    return compiled;
};

// Distribyted.confirm replaces native confirm(), which is unstyled and
// unreliable on mobile. Returns a Promise<boolean> resolving true if the
// user confirmed.
Distribyted.confirm = function (opts) {
    opts = opts || {};
    var modalEl = document.getElementById('distribyted-confirm');
    if (!modalEl) {
        // Fallback for any page that hasn't picked up the updated footer yet.
        return Promise.resolve(window.confirm(opts.body || 'Are you sure?'));
    }

    document.getElementById('distribyted-confirm-title').textContent = opts.title || 'Please confirm';
    document.getElementById('distribyted-confirm-body').textContent = opts.body || 'Are you sure?';

    var confirmBtn = document.getElementById('distribyted-confirm-ok');
    confirmBtn.textContent = opts.confirmLabel || 'Confirm';
    confirmBtn.classList.toggle('btn-danger', !!opts.danger);
    confirmBtn.classList.toggle('btn-primary', !opts.danger);

    var modal = $(modalEl);

    return new Promise(function (resolve) {
        var settled = false;
        var onConfirm = function () {
            settled = true;
            modal.modal('hide');
            resolve(true);
        };
        var onHide = function () {
            confirmBtn.removeEventListener('click', onConfirm);
            modalEl.removeEventListener('hidden.bs.modal', onHide);
            if (!settled) resolve(false);
        };

        confirmBtn.addEventListener('click', onConfirm);
        modalEl.addEventListener('hidden.bs.modal', onHide);
        modal.modal('show');
    });
};

Distribyted.message = {

    _toastr: function () {
        toastr.options = {
            closeButton: true,
            debug: false,
            newestOnTop: false,
            progressBar: true,
            positionClass: "toast-top-right",
            preventDuplicates: false,
            onclick: null,
            showDuration: "300",
            hideDuration: "1000",
            timeOut: "5000",
            extendedTimeOut: "1000",
            showEasing: "swing",
            hideEasing: "linear",
            showMethod: "fadeIn",
            hideMethod: "fadeOut"
        };

        return toastr;
    },


    error: function (message) {
        this._toastr().error(message);
    },

    info: function (message) {
        this._toastr().info(message);
    }
}

$(document).ready(function () {
    "use strict";

    /*======== 1. SCROLLBAR SIDEBAR ========*/
    var sidebarScrollbar = $(".sidebar-scrollbar");
    if (sidebarScrollbar.length != 0) {
        sidebarScrollbar.slimScroll({
            opacity: 0,
            height: "100%",
            color: "#808080",
            size: "5px",
            touchScrollStep: 50
        })
            .mouseover(function () {
                $(this)
                    .next(".slimScrollBar")
                    .css("opacity", 0.5);
            });
    }

    /*======== 2. MOBILE OVERLAY ========*/
    if ($(window).width() < 768) {
        $(".sidebar-toggle").on("click", function () {
            $("body").css("overflow", "hidden");
            $('body').prepend('<div class="mobile-sticky-body-overlay"></div>')
        });

        $(document).on("click", '.mobile-sticky-body-overlay', function (e) {
            $(this).remove();
            $("#body").removeClass("sidebar-mobile-in").addClass("sidebar-mobile-out");
            $("body").css("overflow", "auto");
        });
    }

    /*======== 3. SIDEBAR MENU ========*/
    var sidebar = $(".sidebar")
    if (sidebar.length != 0) {
        $(".sidebar .nav > .has-sub > a").click(function () {
            $(this).parent().siblings().removeClass('expand')
            $(this).parent().toggleClass('expand')
        })

        $(".sidebar .nav > .has-sub .has-sub > a").click(function () {
            $(this).parent().toggleClass('expand')
        })
    }


    /*======== 4. SIDEBAR TOGGLE FOR MOBILE ========*/
    if ($(window).width() < 768) {
        $(document).on("click", ".sidebar-toggle", function (e) {
            e.preventDefault();
            var min = "sidebar-mobile-in",
                min_out = "sidebar-mobile-out",
                body = "#body";
            $(body).hasClass(min)
                ? $(body)
                    .removeClass(min)
                    .addClass(min_out)
                : $(body)
                    .addClass(min)
                    .removeClass(min_out)
        });
    }

    /*======== 5. SIDEBAR TOGGLE FOR VARIOUS SIDEBAR LAYOUT ========*/
    var body = $("#body");
    if ($(window).width() >= 768) {

        if (typeof window.isMinified === "undefined") {
            window.isMinified = false;
        }
        if (typeof window.isCollapsed === "undefined") {
            window.isCollapsed = false;
        }

        $("#sidebar-toggler").on("click", function () {
            if (
                body.hasClass("sidebar-fixed-offcanvas") ||
                body.hasClass("sidebar-static-offcanvas")
            ) {
                $(this)
                    .addClass("sidebar-offcanvas-toggle")
                    .removeClass("sidebar-toggle");
                if (window.isCollapsed === false) {
                    body.addClass("sidebar-collapse");
                    window.isCollapsed = true;
                    window.isMinified = false;
                } else {
                    body.removeClass("sidebar-collapse");
                    body.addClass("sidebar-collapse-out");
                    setTimeout(function () {
                        body.removeClass("sidebar-collapse-out");
                    }, 300);
                    window.isCollapsed = false;
                }
            }

            if (
                body.hasClass("sidebar-fixed") ||
                body.hasClass("sidebar-static")
            ) {
                $(this)
                    .addClass("sidebar-toggle")
                    .removeClass("sidebar-offcanvas-toggle");
                if (window.isMinified === false) {
                    body
                        .removeClass("sidebar-collapse sidebar-minified-out")
                        .addClass("sidebar-minified");
                    window.isMinified = true;
                    window.isCollapsed = false;
                } else {
                    body.removeClass("sidebar-minified");
                    body.addClass("sidebar-minified-out");
                    window.isMinified = false;
                }
            }
        });
    }

    if ($(window).width() >= 768 && $(window).width() < 992) {
        if (
            body.hasClass("sidebar-fixed") ||
            body.hasClass("sidebar-static")
        ) {
            body
                .removeClass("sidebar-collapse sidebar-minified-out")
                .addClass("sidebar-minified");
            window.isMinified = true;
        }
    }
});