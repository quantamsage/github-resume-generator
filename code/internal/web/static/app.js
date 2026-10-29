// Shared page behavior: loading overlays, repository selection limits,
// sample preview and back links.
(function () {
  "use strict";

  // Loading overlay for slow form submissions.
  document.querySelectorAll("form[data-loading]").forEach(function (form) {
    form.addEventListener("submit", function () {
      var overlay = document.getElementById("loading");
      if (!overlay) return;
      document.getElementById("loading-text").textContent = form.getAttribute("data-loading");
      overlay.hidden = false;
      form.querySelectorAll("button[type=submit]").forEach(function (b) { b.disabled = true; });
    });
  });
  // Restore the page if the user navigates back to it from the bfcache.
  window.addEventListener("pageshow", function (e) {
    if (!e.persisted) return;
    var overlay = document.getElementById("loading");
    if (overlay) overlay.hidden = true;
    document.querySelectorAll("button[type=submit]").forEach(function (b) { b.disabled = false; });
  });

  // Repository selection: enforce the maximum and show a count.
  var list = document.querySelector(".repo-list[data-max]");
  if (list) {
    var max = parseInt(list.getAttribute("data-max"), 10) || 8;
    var boxes = list.querySelectorAll("input[type=checkbox]");
    var count = document.getElementById("selected-count");
    var submit = document.querySelector(".analyze-form button[type=submit]");
    var update = function () {
      var n = 0;
      boxes.forEach(function (b) { if (b.checked) n++; });
      boxes.forEach(function (b) { b.disabled = !b.checked && n >= max; });
      if (count) count.textContent = n + " selected.";
      if (submit) submit.disabled = n === 0;
    };
    boxes.forEach(function (b) { b.addEventListener("change", update); });
    update();
  }

  // Landing page sample preview.
  var sample = document.getElementById("sample-preview");
  if (sample && window.ResumeRender) {
    try {
      window.ResumeRender.render(sample, JSON.parse(sample.getAttribute("data-resume")), "modern");
    } catch (e) { /* the sample is decorative */ }
  }

  document.querySelectorAll("[data-back]").forEach(function (a) {
    a.addEventListener("click", function (e) {
      if (history.length > 1) { e.preventDefault(); history.back(); }
    });
  });
})();
