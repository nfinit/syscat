(function () {
    "use strict";

    var urls = window.URL || window.webkitURL;
    var canPreview = urls && urls.createObjectURL && urls.revokeObjectURL;
    var captionCount = 0;
    var forms = document.querySelectorAll(".entry-form");

    function enhanceCaption(photo) {
        var editor = photo.querySelector(".caption-editor");
        var display = photo.querySelector(".caption-display");
        var row = photo.querySelector(".photo-caption");
        var input = editor && editor.querySelector("input");
        if (!input || !display || !row || !input.addEventListener || !("textContent" in display)) { return; }
        var button = document.createElement("button");
        button.type = "button";
        button.className = "caption-toggle";
        button.setAttribute("aria-controls", editor.id);
        var expanded = !!document.querySelector(".error");

        function refresh() {
            display.textContent = input.value;
        }
        function toggle() {
            editor.style.display = expanded ? "block" : "none";
            button.setAttribute("aria-expanded", expanded ? "true" : "false");
            button.textContent = expanded ? "Done" : "Edit caption";
        }
        button.addEventListener("click", function () {
            expanded = !expanded;
            refresh();
            toggle();
            if (expanded) { input.focus(); }
        });
        input.addEventListener("input", refresh);
        input.addEventListener("change", refresh);
        input.form.addEventListener("reset", function () { window.setTimeout(refresh, 0); });
        row.appendChild(button);
        toggle();
    }

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
            if (!input.files || !input.files.length) {
                return;
            }
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

                    var status = document.createElement("p");
                    status.setAttribute("role", "status");
                    status.textContent = "Loading preview...";
                    var photo = document.createElement("div");
                    photo.className = "captioned-photo";
                    photo.appendChild(link);
                    photo.appendChild(status);
                    var row = document.createElement("div");
                    row.className = "photo-caption";
                    var display = document.createElement("span");
                    display.className = "caption-display";
                    row.appendChild(display);
                    var full = document.createElement("a");
                    full.href = url;
                    full.target = "_blank";
                    full.rel = "noopener noreferrer";
                    full.textContent = "Open full image";
                    row.appendChild(full);
                    photo.appendChild(row);
                    var editor = document.createElement("div");
                    editor.className = "caption-editor";
                    var label = document.createElement("label");
                    var caption = document.createElement("input");
                    caption.type = "text";
                    caption.name = "caption_" + input.name;
                    caption.id = "upload-caption-" + (++captionCount);
                    caption.maxLength = 1000;
                    label.htmlFor = caption.id;
                    label.textContent = "Caption for " + file.name + " (optional)";
                    editor.id = caption.id + "-editor";
                    editor.appendChild(label);
                    editor.appendChild(caption);
                    photo.appendChild(editor);
                    preview.appendChild(photo);
                    enhanceCaption(photo);
                    img.onload = function () {
                        if (photo.parentNode !== preview) { return; }
                        link.style.display = "block";
                        status.textContent = "";
                    };
                    img.onerror = function () {
                        if (photo.parentNode !== preview) { return; }
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
            var existing = form.querySelectorAll(".existing-photo");
            for (var k = 0; k < existing.length; k++) { enhanceCaption(existing[k]); }
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
