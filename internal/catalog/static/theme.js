(function () {
    "use strict";
    if (!document.addEventListener) { return; }
    var key = "syscat:theme";
    var preference = null;
    var system = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
    var button;
    try { preference = window.localStorage.getItem(key); } catch (ignore) { /* Storage is optional. */ }
    if (preference !== "light" && preference !== "dark") { preference = null; }

    function apply() {
        var dark = preference ? preference === "dark" : !!(system && system.matches);
        document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
        if (button) { button.textContent = dark ? "Light UI" : "Dark UI"; }
    }
    // Apply before styles and page content load to avoid a flash of the wrong theme.
    apply();
    if (system) {
        if (system.addEventListener) { system.addEventListener("change", apply); }
        else if (system.addListener) { system.addListener(apply); }
    }
    window.addEventListener("storage", function (event) {
        if (event.key !== key && event.key !== null) { return; }
        preference = event.newValue === "light" || event.newValue === "dark" ? event.newValue : null;
        apply();
    });
    document.addEventListener("DOMContentLoaded", function () {
        var footer = document.querySelector(".site-footer");
        if (!footer) { return; }
        button = document.createElement("button");
        button.type = "button";
        button.className = "theme-toggle";
        button.addEventListener("click", function () {
            preference = document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark";
            try { window.localStorage.setItem(key, preference); } catch (ignore) { /* Switching still works. */ }
            apply();
        });
        footer.appendChild(button);
        apply();
    });
}());
