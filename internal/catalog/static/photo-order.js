(function () {
    "use strict";
    var forms = document.querySelectorAll(".entry-form");
    for (var i = 0; i < forms.length; i++) {
        (function (form) {
            var gallery = form.querySelector(".existing-photos");
            if (!gallery || !gallery.addEventListener) { return; }

            function refresh() {
                var photos = gallery.querySelectorAll(".existing-photo");
                for (var j = 0; j < photos.length; j++) {
                    photos[j].querySelector(".photo-position input").value = j + 1;
                    photos[j].querySelector(".photo-heading").textContent = j === 0 ? "Overview photo" : "Detail photo";
                    photos[j].querySelector(".move-photo-up").disabled = j === 0;
                    photos[j].querySelector(".move-photo-down").disabled = j === photos.length - 1;
                }
            }
            var photos = gallery.querySelectorAll(".existing-photo");
            for (var j = 0; j < photos.length; j++) {
                (function (photo) {
                    var controls = document.createElement("div");
                    controls.className = "photo-order-controls";
                    var up = document.createElement("button");
                    up.type = "button";
                    up.className = "photo-move move-photo-up";
                    up.textContent = "Move up";
                    var down = document.createElement("button");
                    down.type = "button";
                    down.className = "photo-move move-photo-down";
                    down.textContent = "Move down";
                    up.addEventListener("click", function () {
                        var previous = photo.previousElementSibling;
                        if (previous) { gallery.insertBefore(photo, previous); refresh(); }
                        (up.disabled ? down : up).focus();
                    });
                    down.addEventListener("click", function () {
                        var next = photo.nextElementSibling;
                        if (next) { gallery.insertBefore(next, photo); refresh(); }
                        (down.disabled ? up : down).focus();
                    });
                    controls.appendChild(up);
                    controls.appendChild(down);
                    photo.insertBefore(controls, photo.querySelector(".photo-position"));
                    photo.querySelector(".photo-position").style.display = "none";
                }(photos[j]));
            }
        }(forms[i]));
        // Set the initial boundary buttons without changing submitted positions.
        var buttons = forms[i].querySelectorAll(".move-photo-up");
        if (buttons.length) { buttons[0].disabled = true; }
        buttons = forms[i].querySelectorAll(".move-photo-down");
        if (buttons.length) { buttons[buttons.length - 1].disabled = true; }
    }
}());
