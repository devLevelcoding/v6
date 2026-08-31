"use strict";
// GoFlare web console — no framework, no build. Hash routes:
//   #/                     project list
//   #/project/<id>         issue stream for a project
//   #/issue/<id>           issue detail (stack, tags, events)

const view = document.getElementById("view");
const crumbs = document.getElementById("crumbs");
const conn = document.getElementById("conn");

const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) =>
  ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

async function api(path, opts) {
  const r = await fetch("/api/0" + path, {
    headers: { "Content-Type": "application/json" }, ...opts,
  });
  conn.textContent = r.ok ? "connected" : "error " + r.status;
  conn.className = "pill" + (r.ok ? " resolved" : " error");
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
  return r.status === 204 ? null : r.json();
}

function ago(ts) {
  if (!ts) return "—";
  const d = (Date.now() - new Date(ts).getTime()) / 1000;
  if (d < 60) return Math.round(d) + "s ago";
  if (d < 3600) return Math.round(d / 60) + "m ago";
  if (d < 86400) return Math.round(d / 3600) + "h ago";
  return Math.round(d / 86400) + "d ago";
}
const num = (n) => (n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(n));

function setCrumbs(parts) {
  crumbs.innerHTML = parts
    .map((p) => (p.href ? `<a href="${p.href}">${esc(p.label)}</a>` : esc(p.label)))
    .join(" / ");
}
function fail(e) {
  view.innerHTML = `<div class="err-banner">${esc(e.message || e)}</div>`;
}

// ---- project list -----------------------------------------------------------
async function routeProjects() {
  setCrumbs([{ label: "Projects" }]);
  view.innerHTML = "loading…";
  const projects = await api("/projects/");
  view.innerHTML = `
    <h1>Projects</h1>
    <p class="sub">A project is what an SDK reports to. Point <code>GOFLARE_DSN</code> at the DSN below.</p>
    <div class="toolbar">
      <input id="np-name" placeholder="New project name">
      <input id="np-platform" placeholder="platform (node, python…)" style="max-width:200px">
      <button class="primary" id="np-go">Create</button>
    </div>
    <div id="plist">${
      projects.length
        ? projects.map(projectCard).join("")
        : `<div class="empty">No projects yet — create one above.</div>`
    }</div>`;
  document.getElementById("np-go").onclick = async () => {
    const name = document.getElementById("np-name").value.trim();
    if (!name) return;
    try {
      await api("/projects/", {
        method: "POST",
        body: JSON.stringify({ name, platform: document.getElementById("np-platform").value.trim() }),
      });
      routeProjects();
    } catch (e) { fail(e); }
  };
}

function projectCard(p) {
  return `<div class="card">
    <div class="row">
      <div class="grow">
        <a class="big" href="#/project/${esc(p.id)}">${esc(p.name)}</a>
        <span class="pill">${esc(p.platform || "unknown")}</span>
        <div class="meta" style="color:var(--dim);font-size:12.5px">slug: ${esc(p.slug)} · id: ${esc(p.id)}</div>
      </div>
      <a href="#/project/${esc(p.id)}">issues →</a>
    </div>
    <div class="dsn" style="margin-top:10px">${esc(p.dsn || "(no key)")}</div>
  </div>`;
}

// ---- issue stream ----------------------------------------------------------
async function routeProject(id) {
  view.innerHTML = "loading…";
  const q = new URLSearchParams(location.hash.split("?")[1] || "");
  const status = q.get("status") || "";
  const query = q.get("query") || "";
  let project, issues;
  try {
    [project, issues] = await Promise.all([
      api("/projects/" + id + "/"),
      api("/projects/" + id + "/issues/?" + new URLSearchParams({ status, query })),
    ]);
  } catch (e) { return fail(e); }
  setCrumbs([{ label: "Projects", href: "#/" }, { label: project.name }]);
  view.innerHTML = `
    <h1>${esc(project.name)}</h1>
    <p class="sub">${issues.length} issue(s) shown</p>
    <div class="toolbar">
      <select id="f-status">
        ${["", "unresolved", "resolved", "ignored"].map((s) =>
          `<option value="${s}" ${s === status ? "selected" : ""}>${s || "all statuses"}</option>`).join("")}
      </select>
      <input id="f-query" placeholder="search title / culprit" value="${esc(query)}">
      <button id="f-go">Filter</button>
    </div>
    <div>${
      issues.length
        ? issues.map(issueRow).join("")
        : `<div class="empty">No issues. Trigger an error in the app to see it here.</div>`
    }</div>`;
  const apply = () => {
    const p = new URLSearchParams();
    if (document.getElementById("f-status").value) p.set("status", document.getElementById("f-status").value);
    if (document.getElementById("f-query").value.trim()) p.set("query", document.getElementById("f-query").value.trim());
    location.hash = "#/project/" + id + (p.toString() ? "?" + p : "");
  };
  document.getElementById("f-go").onclick = apply;
  document.getElementById("f-status").onchange = apply;
  document.getElementById("f-query").addEventListener("keydown", (e) => e.key === "Enter" && apply());
}

function issueRow(i) {
  const lvl = esc(i.level || "error");
  return `<a class="list-item" href="#/issue/${esc(i.id)}">
    <div class="row">
      <div class="grow">
        <div class="title">
          <span class="pill ${lvl}">${lvl}</span>
          ${i.regressed ? `<span class="pill warning">regressed</span>` : ""}
          ${i.status !== "unresolved" ? `<span class="pill ${esc(i.status)}">${esc(i.status)}</span>` : ""}
          ${esc(i.title)}
        </div>
        <div class="meta">${esc(i.culprit || "—")}</div>
      </div>
      <div class="count">${num(i.times_seen)} events<br>${ago(i.last_seen)}</div>
    </div>
  </a>`;
}

// ---- issue detail --------------------------------------------------------
async function routeIssue(id) {
  view.innerHTML = "loading…";
  let issue, ev;
  try {
    issue = await api("/issues/" + id + "/");
    ev = await api("/issues/" + id + "/events/latest/").catch(() => null);
  } catch (e) { return fail(e); }
  setCrumbs([
    { label: "Projects", href: "#/" },
    { label: "issue", href: "#/project/" + issue.project_id },
    { label: issue.title.slice(0, 40) },
  ]);
  const lvl = esc(issue.level || "error");
  view.innerHTML = `
    <h1><span class="pill ${lvl}">${lvl}</span> ${esc(issue.title)}</h1>
    <p class="sub">${esc(issue.culprit || "")}</p>
    <div class="toolbar">
      ${btn(id, "resolved", "Resolve", issue.status)}
      ${btn(id, "ignored", "Ignore", issue.status)}
      ${btn(id, "unresolved", "Unresolve", issue.status)}
    </div>
    <div class="card">
      <dl class="kv">
        <dt>events</dt><dd class="mono">${issue.times_seen}</dd>
        <dt>status</dt><dd>${esc(issue.status)}${issue.regressed ? " (regressed)" : ""}</dd>
        <dt>first seen</dt><dd>${esc(new Date(issue.first_seen).toLocaleString())} (${ago(issue.first_seen)})</dd>
        <dt>last seen</dt><dd>${esc(new Date(issue.last_seen).toLocaleString())} (${ago(issue.last_seen)})</dd>
        <dt>platform</dt><dd>${esc(issue.platform || "—")}</dd>
        <dt>fingerprint</dt><dd class="mono" style="font-size:11px">${esc(issue.hash)}</dd>
      </dl>
    </div>
    ${ev ? eventDetail(ev) : `<div class="empty">No sampled event payload.</div>`}`;
}

function btn(id, status, label, current) {
  const disabled = current === status ? "disabled" : "";
  return `<button data-status="${status}" ${disabled}
    onclick="setStatus('${id}','${status}')">${label}</button>`;
}
window.setStatus = async (id, status) => {
  try {
    await api("/issues/" + id + "/", { method: "PUT", body: JSON.stringify({ status }) });
    routeIssue(id);
  } catch (e) { fail(e); }
};

function eventDetail(ev) {
  const exc = (ev.exceptions || [])[ (ev.exceptions || []).length - 1 ];
  const tags = ev.tags || {};
  return `
    <h2>Latest event</h2>
    <div class="card">
      <dl class="kv">
        <dt>event id</dt><dd class="mono" style="font-size:11px">${esc(ev.event_id)}</dd>
        <dt>time</dt><dd>${esc(new Date(ev.timestamp).toLocaleString())}</dd>
        ${ev.environment ? `<dt>environment</dt><dd>${esc(ev.environment)}</dd>` : ""}
        ${ev.release ? `<dt>release</dt><dd>${esc(ev.release)}</dd>` : ""}
        ${ev.server_name ? `<dt>server</dt><dd>${esc(ev.server_name)}</dd>` : ""}
        ${ev.transaction ? `<dt>transaction</dt><dd>${esc(ev.transaction)}</dd>` : ""}
        ${ev.sdk ? `<dt>sdk</dt><dd>${esc(ev.sdk)}</dd>` : ""}
      </dl>
      ${ev.message ? `<pre class="dsn" style="white-space:pre-wrap;margin-top:10px">${esc(ev.message)}</pre>` : ""}
      ${Object.keys(tags).length
        ? `<div style="margin-top:10px">${Object.entries(tags)
            .map(([k, v]) => `<span class="tag">${esc(k)}=${esc(v)}</span>`).join("")}</div>`
        : ""}
    </div>
    ${exc && exc.frames && exc.frames.length ? `
      <h2>${esc(exc.type || "Exception")}${exc.value ? ": " + esc(exc.value) : ""}</h2>
      <div class="frames">${exc.frames.slice().reverse().map(frameRow).join("")}</div>` : ""}`;
}

function frameRow(f) {
  const loc = [f.filename || f.module, f.lineno && ":" + f.lineno, f.colno && ":" + f.colno]
    .filter(Boolean).join("");
  return `<div class="frame ${f.in_app ? "inapp" : ""}">
    <span class="fn">${esc(f.function || "?")}</span>
    <span class="loc"> — ${esc(loc)}</span>
  </div>`;
}

// ---- router --------------------------------------------------------------
function router() {
  const path = (location.hash.replace(/^#/, "") || "/").split("?")[0];
  const seg = path.split("/").filter(Boolean);
  if (seg[0] === "project" && seg[1]) return routeProject(seg[1]).catch(fail);
  if (seg[0] === "issue" && seg[1]) return routeIssue(seg[1]).catch(fail);
  return routeProjects().catch(fail);
}
window.addEventListener("hashchange", router);
router();
