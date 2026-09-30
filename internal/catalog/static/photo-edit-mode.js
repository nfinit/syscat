(function () {
    "use strict";
    var sections = document.querySelectorAll(".photo-section[data-existing-entry]");
    for (var i = 0; i < sections.length; i++) {
        (function (section, index) {
            var heading = section.querySelector(".section-label");
            if (!heading || !heading.addEventListener) { return; }
            var button = document.createElement("button");
            button.type = "button";
            button.className = "photo-edit-mode-toggle";
            section.id = "photo-section-" + index;
            button.setAttribute("aria-controls", section.id);
            var editing = !!document.querySelector(".error") || !section.querySelector(".existing-photo");
            function refresh() {
                section.setAttribute("data-photo-editing", editing ? "true" : "false");
                button.setAttribute("aria-expanded", editing ? "true" : "false");
                button.textContent = editing ? "Done editing photos" : "Edit photos";
            }
            button.addEventListener("click", function () {
                editing = !editing;
                refresh();
            });
            heading.appendChild(button);
            refresh();
        }(sections[i], i));
    }
}());
