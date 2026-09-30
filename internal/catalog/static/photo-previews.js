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
        row.insertBefore(button, row.querySelector(".group-edit-toggle"));
        toggle();
    }

    function numberUploadChoices(form) {
        var fields = form.querySelectorAll(".photo-inputs input[type=file]");
        var number = 0;
        for (var pass = 0; pass < 2; pass++) {
            for (var i = 0; i < fields.length; i++) {
                if ((fields[i].name === "overview") !== (pass === 0)) { continue; }
                var choices = fields[i].parentNode.querySelectorAll("[data-upload-photo]");
                for (var j = 0; j < choices.length; j++) { choices[j].value = "upload:" + number++; }
            }
        }
        if (!form.querySelector("input[name=overview_choice]:checked")) {
            var first = form.querySelector("input[name=overview_choice]");
            if (first) { first.checked = true; }
        }
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
            if (!input.files || !input.files.length) { numberUploadChoices(input.form); return; }
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
                    var choiceLabel = document.createElement("label");
                    choiceLabel.className = "overview-choice";
                    var choice = document.createElement("input");
                    choice.type = "radio";
                    choice.name = "overview_choice";
                    choice.setAttribute("data-upload-photo", "true");
                    choiceLabel.appendChild(choice);
                    choiceLabel.appendChild(document.createTextNode(" Set as overview"));
                    photo.insertBefore(choiceLabel, row);
                    if (input.name !== "overview") {
                        var groupField = document.createElement("div");
                        groupField.className = "photo-group-field";
                        var groupLabel = document.createElement("label");
                        var groupInput = document.createElement("input");
                        groupInput.type = "text";
                        groupInput.name = "group_" + input.name;
                        groupInput.id = caption.id + "-group";
                        groupInput.maxLength = 100;
                        groupInput.setAttribute("list", "photo-group-names");
                        groupLabel.htmlFor = groupInput.id;
                        groupLabel.textContent = "Group (optional)";
                        groupField.appendChild(groupLabel);
                        groupField.appendChild(groupInput);
                        photo.appendChild(groupField);
                    }
                    preview.appendChild(photo);
                    enhanceCaption(photo);
                    if (window.SyscatPhotoGroups && photo.querySelector(".photo-group-field")) { window.SyscatPhotoGroups.enhanceEditor(photo.querySelector(".photo-group-field")); }
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
            numberUploadChoices(input.form);
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
