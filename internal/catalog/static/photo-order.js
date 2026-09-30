(function () {
    "use strict";
    var forms = document.querySelectorAll(".entry-form");
    for (var i = 0; i < forms.length; i++) {
        (function (form) {
            var gallery = form.querySelector(".existing-photos");
            if (!gallery || !gallery.addEventListener) { return; }
            var rearranging = false;
            function namedSections() {
                var all = gallery.querySelectorAll(".photo-group"), result = [];
                for (var j = 0; j < all.length; j++) {
                    if (all[j].getAttribute("data-group")) { result.push(all[j]); }
                }
                return result;
            }
            function sectionFor(name) {
                var sections = gallery.querySelectorAll(".photo-group");
                for (var j = 0; j < sections.length; j++) {
                    if (!sections[j].getAttribute("data-overview") && sections[j].getAttribute("data-group") === name) { return sections[j]; }
                }
                var section = document.createElement("section");
                section.className = "photo-group";
                section.setAttribute("data-group", name);
                var heading = document.createElement("h3");
                heading.className = "photo-group-heading";
                heading.textContent = name || "Photos";
                section.appendChild(heading);
                if (name) {
                    var position = document.createElement("div");
                    position.className = "group-position";
                    var input = document.createElement("input");
                    input.type = "number";
                    input.name = "group_position_" + name;
                    input.min = 1;
                    input.required = true;
                    position.appendChild(input);
                    section.appendChild(position);
                }
                var body = document.createElement("div");
                body.className = "group-photos";
                section.appendChild(body);
                var ungrouped = null;
                for (var k = 0; k < sections.length; k++) {
                    if (!sections[k].getAttribute("data-overview") && sections[k].getAttribute("data-group") === "") { ungrouped = sections[k]; }
                }
                if (name && ungrouped) { gallery.insertBefore(section, ungrouped); }
                else { gallery.appendChild(section); }
                enhanceSection(section);
                return section;
            }
            function groupField(photo) {
                var field = photo.querySelector(".photo-group-field");
                if (!field) {
                    field = document.createElement("div");
                    field.className = "photo-group-field";
                    var label = document.createElement("label");
                    var input = document.createElement("input");
                    input.type = "text";
                    input.name = "group_" + photo.getAttribute("data-photo");
                    input.id = photo.querySelector(".caption-editor input").id + "-group";
                    input.maxLength = 100;
                    input.setAttribute("list", "photo-group-names");
                    label.htmlFor = input.id;
                    label.textContent = "Group (optional)";
                    field.appendChild(label);
                    field.appendChild(input);
                    photo.appendChild(field);
                    wireGroupInput(input);
                    if (window.SyscatPhotoGroups) { window.SyscatPhotoGroups.enhanceEditor(field); }
                }
                return field;
            }
            function updateGroupSuggestions() {
                var suggestions = form.querySelector("#photo-group-names");
                if (!suggestions) { return; }
                suggestions.textContent = "";
                var names = [], sections = namedSections();
                for (var j = 0; j < sections.length; j++) { names.push(sections[j].getAttribute("data-group")); }
                var inputs = gallery.querySelectorAll(".photo-group-field input");
                for (var k = 0; k < inputs.length; k++) { names.push(inputs[k].value.replace(/^\s+|\s+$/g, "")); }
                var seen = [];
                for (var n = 0; n < names.length; n++) {
                    if (!names[n] || seen.indexOf(names[n]) !== -1) { continue; }
                    seen.push(names[n]);
                    var option = document.createElement("option");
                    option.value = names[n];
                    suggestions.appendChild(option);
                }
            }
            function wireGroupInput(input) {
                input.addEventListener("change", function () {
                    if (rearranging) { return; }
                    // Keep the editing layout stable. Saving applies group changes.
                    input.value = input.value.replace(/^\s+|\s+$/g, "");
                    updateGroupSuggestions();
                });
            }
            function refresh() {
                var sections = gallery.querySelectorAll(".photo-group");
                for (var j = 0; j < sections.length; j++) {
                    if (!sections[j].getAttribute("data-overview") && !sections[j].querySelector(".existing-photo")) { gallery.removeChild(sections[j]); }
                }
                var names = namedSections();
                for (var n = 0; n < names.length; n++) {
                    names[n].querySelector(".group-position input").value = n + 1;
                    names[n].querySelector(".move-group-up").disabled = n === 0;
                    names[n].querySelector(".move-group-down").disabled = n === names.length - 1;
                }
                updateGroupSuggestions();
                sections = gallery.querySelectorAll(".photo-group");
                var position = 1;
                for (var s = 0; s < sections.length; s++) {
                    var photos = sections[s].querySelectorAll(".existing-photo");
                    var overview = !!sections[s].getAttribute("data-overview");
                    for (var p = 0; p < photos.length; p++) {
                        photos[p].querySelector(".photo-position input").value = position++;
                        photos[p].querySelector(".move-photo-up").disabled = overview || p === 0;
                        photos[p].querySelector(".move-photo-down").disabled = overview || p === photos.length - 1;
                        photos[p].querySelector(".set-overview").disabled = overview;
                        var field = photos[p].querySelector(".photo-group-field");
                        if (field) {
                            field.style.display = overview ? "none" : "block";
                            if (window.SyscatPhotoGroups) { window.SyscatPhotoGroups.enhanceEditor(field); }
                            var editGroup = photos[p].querySelector(".group-edit-toggle");
                            if (editGroup) { editGroup.style.display = overview ? "none" : "inline-block"; }
                        }
                    }
                }
            }
            function action(text, className) {
                var button = document.createElement("button");
                button.type = "button";
                button.className = "photo-move " + className;
                button.textContent = text;
                return button;
            }
            function enhanceSection(section) {
                if (window.SyscatPhotoGroups) { window.SyscatPhotoGroups.enhance(section); }
                var field = section.querySelector(".group-position");
                if (!field) { return; }
                field.style.display = "none";
                var controls = document.createElement("div");
                controls.className = "group-order-controls";
                var up = action("Move group up", "move-group-up");
                var down = action("Move group down", "move-group-down");
                up.addEventListener("click", function () {
                    var named = namedSections();
                    for (var j = 1; j < named.length; j++) {
                        if (named[j] === section) { gallery.insertBefore(section, named[j-1]); break; }
                    }
                    refresh(); (up.disabled ? down : up).focus();
                });
                down.addEventListener("click", function () {
                    var named = namedSections();
                    for (var j = 0; j < named.length - 1; j++) {
                        if (named[j] === section) { gallery.insertBefore(named[j+1], section); break; }
                    }
                    refresh(); (down.disabled ? up : down).focus();
                });
                controls.appendChild(up); controls.appendChild(down);
                section.insertBefore(controls, field);
            }
            var sections = gallery.querySelectorAll(".photo-group");
            for (var s = 0; s < sections.length; s++) { enhanceSection(sections[s]); }
            var photos = gallery.querySelectorAll(".existing-photo");
            for (var j = 0; j < photos.length; j++) {
                (function (photo) {
                    var controls = document.createElement("div");
                    controls.className = "photo-order-controls";
                    var up = action("Move up", "move-photo-up");
                    var down = action("Move down", "move-photo-down");
                    var overview = action("Set as overview", "set-overview");
                    up.addEventListener("click", function () {
                        var previous = photo.previousElementSibling;
                        if (previous) { photo.parentNode.insertBefore(photo, previous); refresh(); }
                        (up.disabled ? down : up).focus();
                    });
                    down.addEventListener("click", function () {
                        var next = photo.nextElementSibling;
                        if (next) { photo.parentNode.insertBefore(next, photo); refresh(); }
                        (down.disabled ? up : down).focus();
                    });
                    overview.addEventListener("click", function () {
                        var section = gallery.querySelector("[data-overview]");
                        var old = section.querySelector(".existing-photo");
                        if (old === photo) { return; }
                        rearranging = true;
                        try {
                            var oldField = groupField(old);
                            oldField.querySelector("input").value = "";
                            var field = groupField(photo);
                            field.querySelector("input").value = "";
                            var ungrouped = sectionFor("");
                            ungrouped.querySelector(".group-photos").insertBefore(old, ungrouped.querySelector(".group-photos").firstChild);
                            section.querySelector(".group-photos").appendChild(photo);
                            photo.querySelector("input[name=overview_choice]").checked = true;
                            refresh();
                            section.querySelector(".group-toggle").focus();
                        } finally { rearranging = false; }
                    });
                    controls.appendChild(up); controls.appendChild(down); controls.appendChild(overview);
                    photo.insertBefore(controls, photo.querySelector(".photo-position"));
                    photo.querySelector(".photo-position").style.display = "none";
                    photo.querySelector(".overview-choice").style.display = "none";
                    var input = photo.querySelector(".photo-group-field input");
                    if (input) { wireGroupInput(input); }
                }(photos[j]));
            }
            refresh();
        }(forms[i]));
    }
}());
