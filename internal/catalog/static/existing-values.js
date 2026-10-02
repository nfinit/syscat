(function () {
    "use strict";

    // Keep suggestions in the page: native datalist popups can interrupt typing
    // with an iPad hardware keyboard. Selection is always deliberate.
    function enhance(list, values) {
        var field = document.getElementById(list.getAttribute("data-field"));
        if (!field || !field.addEventListener) { return; }
        var options = list.querySelectorAll("li");
        var matches = [];
        var active = -1;
        var composing = false;
        field.setAttribute("role", "combobox");
        field.setAttribute("aria-autocomplete", "list");
        field.setAttribute("aria-controls", list.id);
        field.setAttribute("aria-expanded", "false");

        function select(index) {
            active = index;
            field.removeAttribute("aria-activedescendant");
            for (var j = 0; j < options.length; j++) {
                options[j].setAttribute("aria-selected", "false");
            }
            if (active >= 0) {
                matches[active].setAttribute("aria-selected", "true");
                field.setAttribute("aria-activedescendant", matches[active].id);
                // Scroll only the list, never the document or the input.
                var item = matches[active];
                var top = item.offsetTop - list.offsetTop;
                if (top < list.scrollTop) { list.scrollTop = top; }
                if (top + item.offsetHeight > list.scrollTop + list.clientHeight) {
                    list.scrollTop = top + item.offsetHeight - list.clientHeight;
                }
            }
        }
        function close() {
            select(-1);
            list.style.display = "none";
            field.setAttribute("aria-expanded", "false");
        }
        function show() {
            // A leading space deliberately requests every existing value.
            var showAll = field.value.charAt(0) === " ";
            var query = field.value.replace(/^\s+|\s+$/g, "").toLowerCase();
            if (!showAll && !query) {
                close();
                matches = [];
                return;
            }
            if (values) {
                var names = values();
                list.textContent = "";
                for (var n = 0; n < names.length; n++) {
                    var option = document.createElement("li");
                    option.textContent = names[n];
                    option.setAttribute("role", "option");
                    list.appendChild(option);
                }
                options = list.querySelectorAll("li");
                wireOptions();
            }
            matches = [];
            for (var j = 0; j < options.length; j++) {
                var match = showAll || options[j].textContent.toLowerCase().indexOf(query) >= 0;
                options[j].style.display = match ? "block" : "none";
                if (match) { matches.push(options[j]); }
            }
            select(-1);
            list.scrollTop = 0;
            list.style.display = matches.length ? "block" : "none";
            field.setAttribute("aria-expanded", matches.length ? "true" : "false");
        }
        function choose(option) {
            field.value = option.textContent;
            field.focus();
            close();
            if (field.setSelectionRange) {
                field.setSelectionRange(field.value.length, field.value.length);
            }
            // Notify other field observers without reopening this list.
            var change = document.createEvent("HTMLEvents");
            change.initEvent("change", true, false);
            field.dispatchEvent(change);
        }
        function wireOptions() {
            for (var j = 0; j < options.length; j++) {
                (function (option, index) {
                    option.id = list.id + "-" + index;
                    option.addEventListener("mousedown", function (event) {
                        event.preventDefault();
                    });
                    option.addEventListener("click", function () { choose(option); });
                }(options[j], j));
            }
        }
        wireOptions();
        field.addEventListener("focus", show);
        field.addEventListener("input", function () { if (!composing) { show(); } });
        field.addEventListener("blur", close);
        field.addEventListener("compositionstart", function () { composing = true; close(); });
        field.addEventListener("compositionend", function () { composing = false; show(); });
        field.addEventListener("keydown", function (event) {
            if (composing || event.isComposing || event.keyCode === 229) { return; }
            if (event.key === "Escape" || event.key === "Tab") {
                if (event.key === "Escape" && field.getAttribute("aria-expanded") === "true") {
                    event.preventDefault();
                }
                close();
            } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                if (field.getAttribute("aria-expanded") !== "true") { show(); }
                if (!matches.length) { return; }
                event.preventDefault();
                var next = active < 0 ? (event.key === "ArrowDown" ? 0 : matches.length - 1) :
                    (active + (event.key === "ArrowDown" ? 1 : -1) + matches.length) % matches.length;
                select(next);
            } else if (event.key === "Enter" && active >= 0) {
                event.preventDefault();
                choose(matches[active]);
            }
        });
    }
    function enhanceGroup(field) {
        if (!field || field.getAttribute("data-suggestions-enhanced")) { return; }
        field.setAttribute("data-suggestions-enhanced", "true");
        field.removeAttribute("list");
        field.setAttribute("autocomplete", "off");
        field.setAttribute("autocorrect", "off");
        field.setAttribute("autocapitalize", "off");
        field.setAttribute("spellcheck", "false");
        var list = document.createElement("ul");
        list.id = field.id + "-suggestions";
        list.className = "value-suggestions";
        list.setAttribute("data-field", field.id);
        list.setAttribute("role", "listbox");
        list.setAttribute("aria-label", "Existing groups");
        field.parentNode.appendChild(list);
        enhance(list, function () {
            var form = field.form;
            var sections = form.querySelectorAll(".photo-group[data-group]");
            var fields = form.querySelectorAll(".photo-group-field input");
            var names = [];
            function add(value) {
                value = value.replace(/^\s+|\s+$/g, "");
                if (value && names.indexOf(value) === -1) { names.push(value); }
            }
            for (var j = 0; j < sections.length; j++) { add(sections[j].getAttribute("data-group")); }
            for (var k = 0; k < fields.length; k++) {
                if (fields[k] !== field) { add(fields[k].value); }
            }
            return names;
        });
    }
    window.SyscatExistingValues = { enhanceGroup: enhanceGroup };
    var lists = document.querySelectorAll(".value-suggestions");
    for (var i = 0; i < lists.length; i++) { enhance(lists[i]); }
    var groups = document.querySelectorAll(".photo-group-field input");
    for (var g = 0; g < groups.length; g++) { enhanceGroup(groups[g]); }
}());
