(function () {
    "use strict";
    var photos = document.querySelectorAll(".existing-photo");
    for (var i = 0; i < photos.length; i++) {
        (function (photo) {
            var field = photo.querySelector(".photo-delete");
            var input = field && field.querySelector("input");
            var row = photo.querySelector(".photo-caption");
            if (!input || !row || !input.addEventListener) { return; }
            var button = document.createElement("button");
            button.type = "button";
            button.className = "photo-delete-toggle";
            var status = document.createElement("span");
            status.className = "photo-delete-status";
            status.setAttribute("role", "status");
            function refresh() {
                photo.classList.toggle("pending-deletion", input.checked);
                button.textContent = input.checked ? "Undo delete" : "Delete photo";
                button.setAttribute("aria-pressed", input.checked ? "true" : "false");
                status.textContent = input.checked ? "Marked for deletion on save." : "";
            }
            button.addEventListener("click", function () {
                input.checked = !input.checked;
                refresh();
            });
            field.style.display = "none";
            row.appendChild(button);
            photo.appendChild(status);
            refresh();
        }(photos[i]));
    }
}());
