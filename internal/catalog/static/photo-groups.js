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
        button.setAttribute("aria-expanded", "true");
        button.addEventListener("click", function () {
            var open = button.getAttribute("aria-expanded") !== "true";
            body.style.display = open ? "block" : "none";
            button.setAttribute("aria-expanded", open ? "true" : "false");
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
