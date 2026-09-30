(function () {
    "use strict";

    // Choosing a previous value is explicit. Do not observe typing or attach a
    // native datalist: its keyboard popup can interrupt hardware-keyboard input.
    var pickers = document.querySelectorAll(".existing-picker");
    for (var i = 0; i < pickers.length; i++) {
        (function (picker) {
            var field = document.getElementById(picker.getAttribute("data-field"));
            var select = picker.querySelector("select");
            var button = picker.querySelector("button");
            if (!field || !select || !button || !button.addEventListener) {
                return;
            }
            button.addEventListener("click", function () {
                if (select.value === "") {
                    return;
                }
                field.value = select.value;
                picker.removeAttribute("open");
                field.focus();
                if (field.setSelectionRange) {
                    field.setSelectionRange(field.value.length, field.value.length);
                }
            });
            picker.style.display = "block";
        }(pickers[i]));
    }
}());
