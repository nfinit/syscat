(function () {
    "use strict";

    var urls = window.URL || window.webkitURL;
    var canPreview = urls && urls.createObjectURL && urls.revokeObjectURL;
    var forms = document.querySelectorAll(".entry-form");

    function attach(input) {
        if (!canPreview || !("files" in input) || !input.addEventListener) { return; }
        var preview = document.createElement("div");
        preview.className = "photo-preview";
        preview.style.display = "none";
        input.parentNode.appendChild(preview);
        var currentURLs = [];

        function update() {
            for (var i = 0; i < currentURLs.length; i++) { urls.revokeObjectURL(currentURLs[i]); }
            currentURLs = [];
            preview.innerHTML = "";
            preview.style.display = "none";
            if (!input.files || !input.files.length) { return; }
            preview.style.display = "block";
            for (var j = 0; j < input.files.length; j++) {
                (function (file) {
                    var url = urls.createObjectURL(file);
                    currentURLs.push(url);
                    var link = document.createElement("a");
                    link.href = url;
                    link.target = "_blank";
                    link.rel = "noopener noreferrer";
                    link.style.display = "none";
                    var img = document.createElement("img");
                    img.alt = "Preview: " + file.name;
                    link.appendChild(img);
                    link.appendChild(document.createTextNode("Open full image"));
                    var status = document.createElement("p");
                    status.setAttribute("role", "status");
                    status.textContent = "Loading preview...";
                    preview.appendChild(link);
                    preview.appendChild(status);
                    img.onload = function () {
                        if (link.parentNode !== preview) { return; }
                        link.style.display = "block";
                        status.textContent = "";
                    };
                    img.onerror = function () {
                        if (link.parentNode !== preview) { return; }
                        status.textContent = "Preview unavailable. File remains selected for upload.";
                    };
                    img.src = url;
                }(input.files[j]));
            }
        }
        input.addEventListener("change", update);
        input.form.addEventListener("reset", function () { window.setTimeout(update, 0); });
    }

    for (var i = 0; i < forms.length; i++) {
        (function (form, index) {
            var container = form.querySelector(".photo-inputs");
            var inputs = container.querySelectorAll("input[type=file]");
            for (var j = 0; j < inputs.length; j++) { attach(inputs[j]); }
            var button = form.querySelector(".add-photo");
            if (!button || !button.addEventListener) { return; }
            var count = 0;
            button.addEventListener("click", function () {
                count++;
                var field = document.createElement("div");
                field.className = "field";
                var label = document.createElement("label");
                var input = document.createElement("input");
                input.type = "file";
                input.id = "photo-extra-" + index + "-" + count;
                input.name = "photos";
                input.accept = "image/jpeg,image/png,image/gif";
                input.multiple = true;
                label.htmlFor = input.id;
                label.appendChild(document.createTextNode("Additional photos"));
                field.appendChild(label);
                field.appendChild(input);
                container.appendChild(field);
                attach(input);
                input.focus();
            });
            button.style.display = "inline-block";
        }(forms[i], i));
    }
}());
