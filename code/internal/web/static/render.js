// Renders a resume object into a container using DOM APIs only (never
// innerHTML), so user- and AI-provided text can't inject markup.
(function () {
  "use strict";

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null && text !== "") n.textContent = text;
    return n;
  }

  function safeHref(u) {
    if (typeof u !== "string") return "";
    u = u.trim();
    return /^(https?:\/\/|mailto:)/i.test(u) && !/[\s"'<>`]/.test(u) ? u : "";
  }

  function link(text, href, cls) {
    var h = safeHref(href);
    if (!h) return el("span", cls, text);
    var a = el("a", cls, text);
    a.href = h;
    a.rel = "noopener noreferrer";
    a.target = "_blank";
    return a;
  }

  function displayURL(u) {
    return String(u || "").replace(/^https?:\/\//i, "").replace(/^www\./i, "").replace(/\/$/, "");
  }

  function dateRange(s, e) {
    if (s && e) return s + " – " + e;
    if (s) return s + " – Present";
    return e || "";
  }

  function list(items) {
    var ul = el("ul", "r-bullets");
    (items || []).forEach(function (t) {
      if (t && String(t).trim()) ul.appendChild(el("li", null, t));
    });
    return ul.childNodes.length ? ul : null;
  }

  function section(root, title, fill) {
    var s = el("section", "r-section");
    s.appendChild(el("h2", "r-heading", title));
    fill(s);
    root.appendChild(s);
  }

  function entryHead(parts, date) {
    var head = el("div", "r-entry-head");
    var left = el("div", "r-entry-title");
    parts.forEach(function (p) { if (p) left.appendChild(p); });
    head.appendChild(left);
    if (date) head.appendChild(el("span", "r-date", date));
    return head;
  }

  function render(container, r, template) {
    container.className = "resume tpl-" + (template || "ats");
    container.textContent = "";
    if (!r) return;
    var ats = template === "ats";

    var header = el("header", "r-header");
    header.appendChild(el("h1", "r-name", r.name || "Your Name"));
    if (r.headline) header.appendChild(el("p", "r-headline", r.headline));
    var c = r.contact || {};
    var items = [];
    if (c.email) items.push(link(c.email, "mailto:" + c.email));
    if (c.phone) items.push(el("span", null, c.phone));
    if (c.location) items.push(el("span", null, c.location));
    [c.github, c.linkedin, c.portfolio, c.website].forEach(function (u) {
      if (u) items.push(link(displayURL(u), u));
    });
    if (items.length) {
      var p = el("p", "r-contact");
      items.forEach(function (n, i) {
        if (i) p.appendChild(el("span", "r-sep", " | "));
        p.appendChild(n);
      });
      header.appendChild(p);
    }
    container.appendChild(header);

    if (r.summary) section(container, "Summary", function (s) { s.appendChild(el("p", null, r.summary)); });

    var skills = (r.skills || []).filter(function (g) { return g.category && g.items && g.items.length; });
    if (skills.length) {
      section(container, "Technical Skills", function (s) {
        skills.forEach(function (g) {
          var p = el("p", "r-skill");
          p.appendChild(el("strong", null, g.category + ": "));
          p.appendChild(document.createTextNode(g.items.join(", ")));
          s.appendChild(p);
        });
      });
    }

    var exp = (r.experience || []).filter(function (e) { return e.title || e.company; });
    if (exp.length) {
      section(container, "Experience", function (s) {
        exp.forEach(function (e) {
          var d = el("div", "r-entry");
          var parts = [el("strong", null, e.title)];
          if (e.company) parts.push(el("span", null, (e.title ? ", " : "") + e.company));
          if (e.location) parts.push(el("span", "r-muted", " — " + e.location));
          d.appendChild(entryHead(parts, dateRange(e.start, e.end)));
          var ul = list(e.highlights);
          if (ul) d.appendChild(ul);
          s.appendChild(d);
        });
      });
    }

    var projects = (r.projects || []).filter(function (p) { return p.name; });
    if (projects.length) {
      section(container, "Selected Projects", function (s) {
        projects.forEach(function (p) {
          var d = el("div", "r-entry");
          var parts = [link(p.name, p.url, "r-project-name")];
          if (p.technologies && p.technologies.length) parts.push(el("span", "r-tech", " | " + p.technologies.join(", ")));
          d.appendChild(entryHead(parts, ""));
          if (p.description) d.appendChild(el("p", null, p.description));
          var ul = list(p.highlights);
          if (ul) d.appendChild(ul);
          var links = el("p", "r-links");
          if (p.url) links.appendChild(link(ats ? displayURL(p.url) : "Source: " + displayURL(p.url), p.url));
          if (p.demo_url) {
            if (links.childNodes.length) links.appendChild(el("span", "r-sep", " | "));
            links.appendChild(link(ats ? displayURL(p.demo_url) : "Live demo", p.demo_url));
          }
          if (links.childNodes.length) d.appendChild(links);
          s.appendChild(d);
        });
      });
    }

    var edu = (r.education || []).filter(function (e) { return e.degree || e.institution; });
    if (edu.length) {
      section(container, "Education", function (s) {
        edu.forEach(function (e) {
          var d = el("div", "r-entry");
          var parts = [el("strong", null, e.degree)];
          if (e.institution) parts.push(el("span", null, (e.degree ? ", " : "") + e.institution));
          d.appendChild(entryHead(parts, dateRange(e.start, e.end)));
          if (e.details) d.appendChild(el("p", null, e.details));
          s.appendChild(d);
        });
      });
    }

    var certs = (r.certifications || []).filter(function (c) { return c.name; });
    if (certs.length) {
      section(container, "Certifications", function (s) {
        certs.forEach(function (c) {
          var parts = [link(c.name, c.url, "r-strong")];
          if (c.issuer) parts.push(el("span", null, ", " + c.issuer));
          s.appendChild(entryHead(parts, c.year || ""));
        });
      });
    }
  }

  window.ResumeRender = { render: render, safeHref: safeHref };
})();
