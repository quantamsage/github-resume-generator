// Resume editor: builds the form from the resume object, keeps a live
// preview in sync, saves to localStorage and requests PDFs from the server.
(function () {
  "use strict";

  var STORAGE_KEY = "resume-generator:v1";
  var app = document.getElementById("editor-app");
  if (!app) return;

  var form = document.getElementById("editor-form");
  var preview = document.getElementById("preview");
  var templateSel = document.getElementById("template");
  var paperSel = document.getElementById("paper");
  var statusEl = document.getElementById("status");

  var state = null;

  // ---------- data helpers ----------

  function arr(v) { return Array.isArray(v) ? v : []; }
  function str(v) { return typeof v === "string" ? v : ""; }
  function strs(v) { return arr(v).filter(function (s) { return typeof s === "string"; }); }

  function normalize(r) {
    r = r && typeof r === "object" ? r : {};
    var c = r.contact || {};
    return {
      name: str(r.name), headline: str(r.headline), summary: str(r.summary),
      contact: {
        email: str(c.email), phone: str(c.phone), location: str(c.location), github: str(c.github),
        website: str(c.website), linkedin: str(c.linkedin), portfolio: str(c.portfolio)
      },
      skills: arr(r.skills).map(function (g) { return { category: str(g.category), items: strs(g.items) }; }),
      projects: arr(r.projects).map(function (p) {
        return {
          name: str(p.name), repo: str(p.repo), url: str(p.url), demo_url: str(p.demo_url),
          description: str(p.description), highlights: strs(p.highlights),
          technologies: strs(p.technologies), facts: strs(p.facts)
        };
      }),
      experience: arr(r.experience).map(function (e) {
        return {
          title: str(e.title), company: str(e.company), location: str(e.location),
          start: str(e.start), end: str(e.end), highlights: strs(e.highlights)
        };
      }),
      education: arr(r.education).map(function (e) {
        return { degree: str(e.degree), institution: str(e.institution), start: str(e.start), end: str(e.end), details: str(e.details) };
      }),
      certifications: arr(r.certifications).map(function (c) {
        return { name: str(c.name), issuer: str(c.issuer), year: str(c.year), url: str(c.url) };
      }),
      inferences: strs(r.inferences),
      missing_info: strs(r.missing_info),
      ai_written: r.ai_written === true
    };
  }

  function splitLines(s) { return s.split("\n").map(function (x) { return x.trim(); }).filter(Boolean); }
  function splitComma(s) { return s.split(",").map(function (x) { return x.trim(); }).filter(Boolean); }

  // ---------- storage ----------

  function save() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({
        resume: state, template: templateSel.value, paper: paperSel.value, savedAt: Date.now()
      }));
      setStatus("Saved in this browser");
    } catch (e) {
      setStatus("Not saved: browser storage is unavailable");
    }
  }

  function load() {
    try {
      var raw = localStorage.getItem(STORAGE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (e) { return null; }
  }

  var statusTimer;
  function setStatus(msg) {
    statusEl.textContent = msg;
    clearTimeout(statusTimer);
    statusTimer = setTimeout(function () { statusEl.textContent = ""; }, 3000);
  }

  var saveTimer;
  function changed() {
    ResumeRender.render(preview, state, templateSel.value);
    clearTimeout(saveTimer);
    saveTimer = setTimeout(save, 400);
  }

  // ---------- form building ----------

  var uid = 0;
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function input(label, value, onInput, opts) {
    opts = opts || {};
    var id = "f" + (++uid);
    var wrap = el("div", "field" + (opts.wide ? " wide" : ""));
    var lab = el("label", null, label);
    lab.htmlFor = id;
    var field;
    if (opts.rows) {
      field = el("textarea");
      field.rows = opts.rows;
    } else {
      field = el("input");
      field.type = opts.type || "text";
    }
    field.id = id;
    field.value = value || "";
    if (opts.placeholder) field.placeholder = opts.placeholder;
    field.maxLength = opts.max || 1200;
    field.addEventListener("input", function () { onInput(field.value); changed(); });
    wrap.appendChild(lab);
    wrap.appendChild(field);
    if (opts.hint) {
      var hint = el("p", "hint", opts.hint);
      hint.id = id + "-hint";
      field.setAttribute("aria-describedby", hint.id);
      wrap.appendChild(hint);
    }
    return wrap;
  }

  function group(title, intro) {
    var fs = el("fieldset", "group");
    fs.appendChild(el("legend", null, title));
    if (intro) fs.appendChild(el("p", "hint", intro));
    var grid = el("div", "fields");
    fs.appendChild(grid);
    form.appendChild(fs);
    return { fs: fs, grid: grid };
  }

  function smallBtn(text, label, onClick, disabled) {
    var b = el("button", "btn btn-small", text);
    b.type = "button";
    b.setAttribute("aria-label", label);
    b.disabled = !!disabled;
    b.addEventListener("click", onClick);
    return b;
  }

  // Renders an editable, reorderable list of cards.
  function listEditor(title, intro, items, titleOf, fill, make, addLabel) {
    var g = group(title, intro);
    items.forEach(function (item, i) {
      var card = el("div", "card");
      var head = el("div", "card-head");
      head.appendChild(el("span", "card-title", titleOf(item) || "Untitled"));
      var tools = el("div", "card-tools");
      tools.appendChild(smallBtn("↑", "Move " + (titleOf(item) || "item") + " up", function () {
        items.splice(i - 1, 0, items.splice(i, 1)[0]); rebuild(); changed();
      }, i === 0));
      tools.appendChild(smallBtn("↓", "Move " + (titleOf(item) || "item") + " down", function () {
        items.splice(i + 1, 0, items.splice(i, 1)[0]); rebuild(); changed();
      }, i === items.length - 1));
      tools.appendChild(smallBtn("Remove", "Remove " + (titleOf(item) || "item"), function () {
        items.splice(i, 1); rebuild(); changed();
      }));
      head.appendChild(tools);
      card.appendChild(head);
      var fields = el("div", "fields");
      fill(fields, item, function () { head.firstChild.textContent = titleOf(item) || "Untitled"; });
      card.appendChild(fields);
      g.grid.appendChild(card);
    });
    var add = el("button", "btn btn-add", addLabel);
    add.type = "button";
    add.addEventListener("click", function () { items.push(make()); rebuild(); changed(); });
    g.fs.appendChild(add);
  }

  function notes() {
    var hasInf = state.inferences.length, hasMissing = state.missing_info.length;
    if (!state.ai_written && !hasInf && !hasMissing) return;
    var box = el("section", "notes");
    box.setAttribute("aria-label", "Review notes");
    if (state.ai_written) {
      box.appendChild(el("p", "notes-lead",
        "The headline, summary, skills and project text were written by AI from your repositories. " +
        "Review every line, and remove anything that isn't accurate."));
    }
    if (hasInf) {
      box.appendChild(el("h3", null, "AI inferences to check"));
      var ul = el("ul");
      state.inferences.forEach(function (t) { ul.appendChild(el("li", null, t)); });
      box.appendChild(ul);
    }
    if (hasMissing) {
      box.appendChild(el("h3", null, "Information to add"));
      var ul2 = el("ul");
      state.missing_info.forEach(function (t) { ul2.appendChild(el("li", null, t)); });
      box.appendChild(ul2);
    }
    form.appendChild(box);
  }

  function rebuild() {
    form.textContent = "";
    uid = 0;
    notes();

    var b = group("Basics");
    b.grid.appendChild(input("Full name", state.name, function (v) { state.name = v; }, { max: 120 }));
    b.grid.appendChild(input("Headline", state.headline, function (v) { state.headline = v; }, { max: 120, placeholder: "Backend Engineer — Go, PostgreSQL" }));
    b.grid.appendChild(input("Professional summary", state.summary, function (v) { state.summary = v; }, { rows: 4, wide: true, max: 1200 }));

    var c = group("Contact & links");
    var ct = state.contact;
    [["Email", "email", "email"], ["Phone", "phone", "tel"], ["Location", "location", "text"],
     ["GitHub", "github", "url"], ["LinkedIn", "linkedin", "url"], ["Portfolio", "portfolio", "url"], ["Website", "website", "url"]
    ].forEach(function (f) {
      c.grid.appendChild(input(f[0], ct[f[1]], function (v) { ct[f[1]] = v; }, { type: f[2], max: 300 }));
    });

    listEditor("Technical skills", "Separate items with commas.", state.skills,
      function (g) { return g.category; },
      function (fields, g, retitle) {
        fields.appendChild(input("Category", g.category, function (v) { g.category = v; retitle(); }, { max: 60 }));
        fields.appendChild(input("Skills", g.items.join(", "), function (v) { g.items = splitComma(v); }, { rows: 2, wide: true }));
      },
      function () { return { category: "", items: [] }; }, "Add skill group");

    listEditor("Projects", "Highlights go one per line. GitHub facts are shown for reference and are not printed.", state.projects,
      function (p) { return p.name; },
      function (fields, p, retitle) {
        fields.appendChild(input("Project name", p.name, function (v) { p.name = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Repository URL", p.url, function (v) { p.url = v; }, { type: "url", max: 300 }));
        fields.appendChild(input("Live demo URL", p.demo_url, function (v) { p.demo_url = v; }, { type: "url", max: 300 }));
        fields.appendChild(input("Technologies", p.technologies.join(", "), function (v) { p.technologies = splitComma(v); }, { max: 400 }));
        fields.appendChild(input("Description", p.description, function (v) { p.description = v; }, { rows: 2, wide: true }));
        fields.appendChild(input("Highlights", p.highlights.join("\n"), function (v) { p.highlights = splitLines(v); }, { rows: 4, wide: true, max: 2400 }));
        if (p.facts.length) {
          var f = el("p", "facts wide");
          f.appendChild(el("strong", null, "Verified from GitHub: "));
          f.appendChild(document.createTextNode(p.facts.join(" · ")));
          fields.appendChild(f);
        }
      },
      function () { return { name: "", repo: "", url: "", demo_url: "", description: "", highlights: [], technologies: [], facts: [] }; },
      "Add project");

    listEditor("Experience", "Only add roles you actually held. Highlights go one per line.", state.experience,
      function (e) { return [e.title, e.company].filter(Boolean).join(" at "); },
      function (fields, e, retitle) {
        fields.appendChild(input("Job title", e.title, function (v) { e.title = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Company", e.company, function (v) { e.company = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Location", e.location, function (v) { e.location = v; }, { max: 120 }));
        fields.appendChild(input("Start", e.start, function (v) { e.start = v; }, { max: 30, placeholder: "Jan 2023" }));
        fields.appendChild(input("End", e.end, function (v) { e.end = v; }, { max: 30, placeholder: "Present" }));
        fields.appendChild(input("Highlights", e.highlights.join("\n"), function (v) { e.highlights = splitLines(v); }, { rows: 4, wide: true, max: 2400 }));
      },
      function () { return { title: "", company: "", location: "", start: "", end: "", highlights: [] }; },
      "Add experience");

    listEditor("Education", null, state.education,
      function (e) { return e.degree || e.institution; },
      function (fields, e, retitle) {
        fields.appendChild(input("Degree or program", e.degree, function (v) { e.degree = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Institution", e.institution, function (v) { e.institution = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Start", e.start, function (v) { e.start = v; }, { max: 30 }));
        fields.appendChild(input("End", e.end, function (v) { e.end = v; }, { max: 30 }));
        fields.appendChild(input("Details", e.details, function (v) { e.details = v; }, { wide: true, max: 300 }));
      },
      function () { return { degree: "", institution: "", start: "", end: "", details: "" }; },
      "Add education");

    listEditor("Certifications", null, state.certifications,
      function (c) { return c.name; },
      function (fields, c, retitle) {
        fields.appendChild(input("Name", c.name, function (v) { c.name = v; retitle(); }, { max: 120 }));
        fields.appendChild(input("Issuer", c.issuer, function (v) { c.issuer = v; }, { max: 120 }));
        fields.appendChild(input("Year", c.year, function (v) { c.year = v; }, { max: 20 }));
        fields.appendChild(input("Credential URL", c.url, function (v) { c.url = v; }, { type: "url", max: 300 }));
      },
      function () { return { name: "", issuer: "", year: "", url: "" }; },
      "Add certification");
  }

  // ---------- actions ----------

  function start(resume, template, paper) {
    state = normalize(resume);
    if (template) templateSel.value = template;
    if (paper) paperSel.value = paper;
    document.getElementById("empty-state").hidden = true;
    document.getElementById("editor-grid").hidden = false;
    rebuild();
    ResumeRender.render(preview, state, templateSel.value);
  }

  templateSel.addEventListener("change", changed);
  paperSel.addEventListener("change", function () {
    document.body.classList.toggle("paper-letter", paperSel.value === "letter");
    changed();
  });

  document.getElementById("download-pdf").addEventListener("click", function () {
    if (!state) return;
    var btn = this;
    btn.disabled = true;
    setStatus("Preparing PDF…");
    fetch("/api/pdf", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ resume: state, template: templateSel.value, paper: paperSel.value })
    }).then(function (res) {
      if (!res.ok) {
        return res.json().catch(function () { return {}; }).then(function (j) {
          throw new Error(j.error || "The PDF could not be generated (" + res.status + ").");
        });
      }
      var name = (res.headers.get("Content-Disposition") || "").match(/filename="([^"]+)"/);
      return res.blob().then(function (b) { return { blob: b, name: name ? name[1] : "resume.pdf" }; });
    }).then(function (r) {
      var url = URL.createObjectURL(r.blob);
      var a = document.createElement("a");
      a.href = url;
      a.download = r.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(function () { URL.revokeObjectURL(url); }, 10000);
      setStatus("PDF downloaded");
    }).catch(function (err) {
      setStatus(err.message);
    }).finally(function () { btn.disabled = false; });
  });

  document.getElementById("print").addEventListener("click", function () { window.print(); });

  document.getElementById("export-json").addEventListener("click", function () {
    if (!state) return;
    var blob = new Blob([JSON.stringify(state, null, 2)], { type: "application/json" });
    var a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "resume.json";
    document.body.appendChild(a);
    a.click();
    a.remove();
  });

  document.getElementById("import-json").addEventListener("change", function () {
    var file = this.files && this.files[0];
    if (!file) return;
    if (file.size > 256 * 1024) { setStatus("That file is too large."); return; }
    var reader = new FileReader();
    reader.onload = function () {
      try {
        start(JSON.parse(reader.result));
        save();
        setStatus("Imported");
      } catch (e) {
        setStatus("That file isn't a valid resume JSON export.");
      }
    };
    reader.readAsText(file);
    this.value = "";
  });

  document.getElementById("clear-data").addEventListener("click", function () {
    if (!confirm("Delete the resume saved in this browser? This can't be undone.")) return;
    try { localStorage.removeItem(STORAGE_KEY); } catch (e) { /* ignore */ }
    state = null;
    form.textContent = "";
    preview.textContent = "";
    document.getElementById("editor-grid").hidden = true;
    document.getElementById("empty-state").hidden = false;
    setStatus("Deleted");
  });

  // ---------- init ----------

  var serverData = app.getAttribute("data-resume");
  var saved = load();
  if (serverData) {
    try {
      start(JSON.parse(serverData), saved && saved.template, saved && saved.paper);
      save();
      return;
    } catch (e) { /* fall through to saved data */ }
  }
  if (saved && saved.resume) {
    start(saved.resume, saved.template, saved.paper);
  } else {
    document.getElementById("editor-grid").hidden = true;
    document.getElementById("empty-state").hidden = false;
  }
})();
