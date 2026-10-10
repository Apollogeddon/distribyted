// Applies the theme chosen with the theme button before the page draws. Without a choice,
// the page follows the system's light or dark setting.
(function () {
    "use strict";
    var root = document.documentElement;
    var chosen = null;
    try { chosen = localStorage.getItem("theme"); } catch (e) { /* storage blocked */ }
    if (chosen === "light" || chosen === "dark") root.dataset.theme = chosen;
    var dark = window.matchMedia("(prefers-color-scheme: dark)");
    function resolve() {
        root.dataset.themeCurrent = root.dataset.theme || (dark.matches ? "dark" : "light");
    }
    resolve();
    dark.addEventListener("change", resolve);
    window.DistribytedTheme = { resolve: resolve };
})();
