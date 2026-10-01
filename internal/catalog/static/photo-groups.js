(function () {
    "use strict";
    var nextID = 0;
    function enhance(section) {
        if (section.getAttribute("data-collapsible")) { return; }
        var heading = section.querySelector(".photo-group-heading");
        var body = section.querySelector(".group-photos");
        if (!heading || !body || !heading.addEventListener || !("textContent" in heading)) { return; }
        var button = document.createElement("button");
        button.type = "button";
        button.className = "group-toggle";
        button.textContent = heading.textContent;
        body.id = "photo-group-body-" + (++nextID);
        button.setAttribute("aria-controls", body.id);
        var gallery = section.parentNode;
        while (gallery && gallery.getAttribute && !gallery.getAttribute("data-photo-asset")) { gallery = gallery.parentNode; }
        var key = gallery && gallery.getAttribute && gallery.getAttribute("data-photo-asset");
        if (key) {
            key = "syscat:photo-group:" + JSON.stringify([key, section.getAttribute("data-overview") ? "overview" : "group", section.getAttribute("data-group") || ""]);
        }
        var open = true;
        try {
            if (key && !document.querySelector(".error")) { open = window.localStorage.getItem(key) !== "collapsed"; }
        } catch (ignore) { /* Storage may be disabled; collapse still works. */ }
        body.style.display = open ? "block" : "none";
        button.setAttribute("aria-expanded", open ? "true" : "false");
        button.addEventListener("click", function () {
            var open = button.getAttribute("aria-expanded") !== "true";
            body.style.display = open ? "block" : "none";
            button.setAttribute("aria-expanded", open ? "true" : "false");
            try {
                if (key) { window.localStorage.setItem(key, open ? "expanded" : "collapsed"); }
            } catch (ignore) { /* Do not interrupt browsers that block storage. */ }
        });
        heading.textContent = "";
        heading.appendChild(button);
        section.setAttribute("data-collapsible", "true");
    }
    function enhanceEditor(field) {
        var input = field.querySelector("input");
        if (!input || !input.addEventListener || !("textContent" in field)) { return; }
        if (field.getAttribute("data-editor-enhanced")) { return; }
        var row = field.parentNode.querySelector(".photo-caption");
        if (!row) { return; }
        var editor = field.querySelector(".group-editor");
        if (!editor) {
            editor = document.createElement("div");
            editor.className = "group-editor";
            while (field.firstChild) { editor.appendChild(field.firstChild); }
            field.appendChild(editor);
        }
        if (window.SyscatExistingValues) { window.SyscatExistingValues.enhanceGroup(input); }
        editor.id = input.id + "-editor";
        var button = document.createElement("button");
        button.type = "button";
        button.className = "group-edit-toggle";
        button.setAttribute("aria-controls", editor.id);
        var expanded = !!document.querySelector(".error");
        function toggle() {
            editor.style.display = expanded ? "block" : "none";
            button.setAttribute("aria-expanded", expanded ? "true" : "false");
            button.textContent = expanded ? "Done" : "Edit group";
        }
        button.addEventListener("click", function () {
            expanded = !expanded;
            toggle();
            if (expanded) { input.focus(); }
        });
        input.addEventListener("keydown", function (event) {
            if (event.isComposing || event.keyCode === 229) { return; }
            if (event.key === "Enter" || event.keyCode === 13) {
                // Let the suggestion widget accept an explicitly highlighted group.
                if (event.defaultPrevented || (input.getAttribute("aria-expanded") === "true" && input.getAttribute("aria-activedescendant"))) { return; }
                event.preventDefault();
                expanded = false;
                toggle();
                button.focus();
            }
        });
        row.appendChild(button);
        field.setAttribute("data-editor-enhanced", "true");
        toggle();
    }
    window.SyscatPhotoGroups = { enhance: enhance, enhanceEditor: enhanceEditor };
    var sections = document.querySelectorAll(".photo-group");
    for (var i = 0; i < sections.length; i++) { enhance(sections[i]); }
    var fields = document.querySelectorAll(".photo-group-field");
    for (var j = 0; j < fields.length; j++) { enhanceEditor(fields[j]); }
}());
