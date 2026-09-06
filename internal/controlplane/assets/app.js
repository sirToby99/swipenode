"use strict";

const state = { csrf: "", activeView: "overview", pendingFilter: "" };
const content = document.getElementById("content");
const notice = document.getElementById("notice");
const pageTitle = document.getElementById("page-title");
const PAGE_SIZE = 50;

function el(tag, className = "", text = null) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== null) node.textContent = String(text);
  return node;
}

function add(parent, ...children) {
  children.flat().filter(Boolean).forEach((child) => parent.appendChild(child));
  return parent;
}

const empty = (text) => el("div", "empty", text);
const shortHash = (value) => value ? `${value.slice(0, 12)}…` : "—";
const fmt = (value) => value ? new Date(value).toLocaleString() : "—";
const status = (value) => el("span", `status ${String(value || "unknown").toLowerCase()}`, value || "UNKNOWN");
const section = (name, right = null) => add(el("div", "section-title"), el("h2", "", name), right);

function textButton(label, view, filter = "") {
  const button = el("button", "text-action", label);
  button.type = "button";
  button.addEventListener("click", () => {
    state.pendingFilter = filter;
    load(view);
  });
  return button;
}

function copyValue(value, abbreviated = false) {
  if (!value) return el("span", "muted", "—");
  const row = el("span", "copy-row");
  const code = el("code", "", abbreviated ? shortHash(value) : value);
  code.title = value;
  const button = el("button", "copy", "Copy");
  button.type = "button";
  button.setAttribute("aria-label", `Copy ${value}`);
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(value);
      button.textContent = "Copied";
      setTimeout(() => { button.textContent = "Copy"; }, 1200);
    } catch {
      showNotice("Clipboard access is unavailable. Select the value manually.", true);
    }
  });
  return add(row, code, button);
}

function safeLink(raw) {
  if (!raw) return el("span", "muted", "—");
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:") return el("span", "muted", raw);
    const link = el("a", "safe-link", raw);
    link.href = parsed.href;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    return link;
  } catch {
    return el("span", "muted", "Invalid URL");
  }
}

function kv(entries) {
  const list = el("dl", "kv");
  entries.forEach(([key, value]) => {
    list.appendChild(el("dt", "", key));
    list.appendChild(value instanceof Node ? add(el("dd"), value) : el("dd", "", value ?? "—"));
  });
  return list;
}

function metric(label, value) {
  return add(el("div", "metric"), el("span", "", label), el("strong", "", value));
}

function panel(name, body, right = null) {
  return add(el("section", "panel"), section(name, right), add(el("div", "panel-body"), body));
}

function showNotice(message, isError = false) {
  notice.replaceChildren();
  if (message) notice.appendChild(el("div", `notice${isError ? " error" : ""}`, message));
}

async function api(path, options = {}) {
  const headers = { Accept: "application/json", ...(options.headers || {}) };
  if (options.method === "POST") {
    headers["Content-Type"] = "application/json";
    headers["X-SwipeNode-CSRF"] = state.csrf;
  }
  const response = await fetch(path, { ...options, headers, credentials: "same-origin" });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error?.message || `Request failed (${response.status})`);
  return data;
}

function action(label, handler, primary = false) {
  const button = el("button", `action${primary ? " primary" : ""}`, label);
  button.type = "button";
  button.addEventListener("click", async () => {
    button.disabled = true;
    try {
      const result = await handler();
      showNotice(typeof result === "string" ? result : `${label}: complete`);
    } catch (error) {
      showNotice(error.message, true);
    } finally {
      button.disabled = false;
    }
  });
  return button;
}

function searchable(items, options) {
  const root = el("div");
  const toolbar = el("div", "toolbar");
  const search = el("input");
  search.type = "search";
  search.placeholder = options.placeholder || "Filter local records";
  search.setAttribute("aria-label", search.placeholder);
  search.value = state.pendingFilter;
  state.pendingFilter = "";
  const filter = el("select");
  filter.setAttribute("aria-label", "Filter by status");
  add(filter, option("all", "All statuses"));
  (options.statuses || []).forEach((value) => filter.appendChild(option(value, value)));
  const count = el("span", "result-count");
  add(toolbar, search, ...(options.statuses?.length ? [filter] : []), count);
  const list = el("div");
  const pager = el("div", "pagination");
  let page = 0;

  function render() {
    const query = search.value.trim().toLowerCase();
    const selected = filter.value;
    const matches = items.filter((item) => {
      const text = options.searchText(item).toLowerCase();
      const itemStatus = String(options.statusValue?.(item) || "").toLowerCase();
      return (!query || text.includes(query)) && (selected === "all" || itemStatus === selected.toLowerCase());
    });
    const pages = Math.max(1, Math.ceil(matches.length / PAGE_SIZE));
    page = Math.min(page, pages - 1);
    list.replaceChildren();
    const visible = matches.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);
    if (!visible.length) list.appendChild(empty(options.emptyText));
    visible.forEach((item) => list.appendChild(options.render(item)));
    count.textContent = `${matches.length} result${matches.length === 1 ? "" : "s"}`;
    pager.replaceChildren();
    if (pages > 1) {
      const previous = action("Previous", () => { page -= 1; render(); });
      previous.disabled = page === 0;
      const next = action("Next", () => { page += 1; render(); });
      next.disabled = page >= pages - 1;
      add(pager, previous, el("span", "muted", `Page ${page + 1} of ${pages}`), next);
    }
  }
  search.addEventListener("input", () => { page = 0; render(); });
  filter.addEventListener("change", () => { page = 0; render(); });
  render();
  return add(root, toolbar, list, pager);
}

function remoteSearchable(initial, options) {
  const root = el("div");
  const toolbar = el("div", "toolbar");
  const search = el("input");
  search.type = "search";
  search.placeholder = options.placeholder;
  search.setAttribute("aria-label", options.placeholder);
  search.value = state.pendingFilter;
  state.pendingFilter = "";
  const filter = el("select");
  filter.setAttribute("aria-label", "Filter by status");
  add(filter, option("", "All statuses"));
  (options.statuses || []).forEach((value) => filter.appendChild(option(value, value)));
  const count = el("span", "result-count");
  add(toolbar, search, ...(options.statuses?.length ? [filter] : []), count);
  const list = el("div");
  const pager = el("div", "pagination");
  let offset = 0;
  let timer = 0;

  function show(payload) {
    const items = options.items(payload);
    const total = payload.total_count ?? items.length;
    list.replaceChildren();
    if (!items.length) list.appendChild(empty(options.emptyText));
    items.forEach((item) => list.appendChild(options.render(item)));
    count.textContent = `${total} result${total === 1 ? "" : "s"}`;
    pager.replaceChildren();
    if (total > PAGE_SIZE) {
      const previous = action("Previous", async () => { offset = Math.max(0, offset - PAGE_SIZE); await refresh(); });
      previous.disabled = offset === 0;
      const next = action("Next", async () => { offset += PAGE_SIZE; await refresh(); });
      next.disabled = offset + items.length >= total;
      add(pager, previous, el("span", "muted", `${offset + 1}–${Math.min(offset + items.length, total)} of ${total}`), next);
    }
  }

  async function refresh() {
    const parameters = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(offset) });
    if (search.value.trim()) parameters.set("q", search.value.trim());
    if (filter.value) parameters.set("status", filter.value);
    try {
      show(await api(`${options.endpoint}?${parameters}`));
    } catch (error) {
      showNotice(error.message, true);
    }
  }

  search.addEventListener("input", () => {
    offset = 0;
    clearTimeout(timer);
    timer = setTimeout(refresh, 180);
  });
  filter.addEventListener("change", () => { offset = 0; refresh(); });
  add(root, toolbar, list, pager);
  if (search.value) refresh();
  else show(initial);
  return root;
}

function option(value, label) {
  const item = el("option", "", label);
  item.value = value;
  return item;
}

function provenanceCard(item, compact = false) {
  const artifacts = (item.artifacts || []).map((value) => value.path).join(", ") || "No artifact";
  const card = add(
    el("article", `record${compact ? "" : " history"}`),
    add(el("div", "record-head"), el("h3", "artifact", artifacts), status(item.adapter === "entire" ? "Entire" : "SwipeNode")),
    kv([
      ["Provenance ID", copyValue(item.provenance_id)],
      ["Verification", textButton(item.verification?.record_id || item.verification?.id || "—", "verifications", item.verification?.record_id || item.verification?.id || "")],
      ["Git", `${item.repository?.git_branch || "—"} · ${shortHash(item.repository?.git_commit)}`],
      ["Packs", (item.knowledge_pack_ids || []).join(", ") || "—"],
      ["Evidence", (item.evidence_ids || []).length ? textButton(`${item.evidence_ids.length} linked record(s)`, "evidence", item.evidence_ids[0]) : "—"],
      ["Recorded", fmt(item.timestamp)],
      ["Entire", item.external_context?.entire?.context_status || "Optional / not present"],
    ]),
  );
  if (!compact) {
    (item.claims || []).forEach((claim) => card.appendChild(add(
      el("div", "source"),
      add(el("div", "record-head"), el("strong", "", claim.statement), status(claim.status)),
      el("p", "compact muted", `${claim.artifact_path}:${claim.line}`),
    )));
  }
  return card;
}

function auditCard(item) {
  return add(
    el("article", "record"),
    add(el("div", "record-head"), el("h3", "", item.event), status(item.new_status || item.failure_kind || item.check_method || "event")),
    kv([
      ["Event ID", copyValue(item.id)],
      ["Pack / source", `${item.pack_id || "—"} / ${item.source_id || "—"}`],
      ["Revision", `${item.old_revision || "—"} → ${item.new_revision || "—"}`],
      ["Hash", `${shortHash(item.old_hash)} → ${shortHash(item.new_hash)}`],
      ["Verification", item.verification_id ? textButton(item.verification_id, "verifications", item.verification_id) : "—"],
      ["Artifact", item.artifact_path || "—"],
      ["Recorded", fmt(item.timestamp)],
    ]),
  );
}

function renderOverview(data) {
  const currentItems = data.current_verifications || [];
  const root = el("div");
  add(root, add(
    el("div", "grid metrics"),
    metric("Knowledge Packs", data.pack_count),
    metric("Installed", data.installed_pack_count),
    metric("Active", data.active_pack_count),
    metric("Sources", data.source_count),
    metric("Current conflicts", data.conflict_count),
    metric("Current unverified", data.unverified_count),
  ));

  const currentBody = el("div");
  if (!currentItems.length) currentBody.appendChild(empty("No verification records exist for this repository."));
  currentItems.slice(0, 10).forEach((current) => {
    const card = add(
      el("article", "record current-state"),
      add(el("div", "record-head"), add(el("div"), el("h3", "", current.statement), el("p", "compact muted", `${current.artifact_path} · ${current.pack_id || "No Pack"}`)), status(current.status)),
      kv([
        ["Verification", textButton(current.verification_id, "verifications", current.verification_id)],
        ["Recorded", fmt(current.recorded_at)],
      ]),
    );
    if (current.history?.length) {
      const flow = add(el("div", "history-flow"), el("strong", "", "History:"));
      [...current.history].reverse().slice(-4).forEach((entry, index) => {
        if (index) flow.appendChild(el("span", "truth-arrow", "→"));
        flow.appendChild(status(entry.status));
      });
      flow.append(el("span", "truth-arrow", "→"), status(current.status));
      card.appendChild(flow);
    }
    currentBody.appendChild(card);
  });

  const healthBody = kv([
    ["Healthy", data.knowledge_health.healthy],
    ["Changed", data.knowledge_health.changed],
    ["Stale", data.knowledge_health.stale],
    ["Failed", data.knowledge_health.failed],
    ["Available managed updates", data.available_updates],
  ]);
  root.appendChild(add(el("div", "grid two-column"), panel("Current verification state", currentBody), panel("Knowledge health", healthBody)));

  const provenanceBody = el("div");
  const provenanceItems = data.recent_provenance || [];
  if (!provenanceItems.length) provenanceBody.appendChild(empty("No Engineering Provenance has been recorded."));
  provenanceItems.slice(0, 4).forEach((item) => provenanceBody.appendChild(provenanceCard(item, true)));
  const auditBody = el("div");
  const auditItems = data.recent_audit || [];
  if (!auditItems.length) auditBody.appendChild(empty("No audit events exist for this repository."));
  auditItems.slice(0, 6).forEach((item) => auditBody.appendChild(auditCard(item)));
  root.appendChild(add(el("div", "grid two-column"), panel("Recent provenance", provenanceBody, textButton("View all", "provenance")), panel("Recent activity", auditBody, textButton("View audit", "audit"))));
  content.replaceChildren(root);
}

function packCard(pack, governance = false) {
  const card = el("article", "record");
  add(card,
    add(el("div", "record-head"), add(el("div"), el("h3", "", pack.id), el("p", "compact muted", `${pack.owner || "Unknown owner"} · ${pack.description || "Installed managed Pack"}`)), status(pack.category)),
    kv([
      ["Origin", pack.origin],
      ["Scope", [...(pack.scope?.topics || []), ...(pack.scope?.components || [])].join(", ") || "—"],
      ["Active", pack.active?.version || "—"],
      ["Installed", (pack.installed_versions || []).map((item) => item.version).join(", ") || "—"],
      ["Available", pack.available_version || "Not checked"],
      ["Freshness", pack.freshness?.suggested_interval || "—"],
      ["Authority provenance", pack.require_provenance ? "Required" : "Not required"],
    ]),
  );
  if (!governance) {
    const controls = el("div", "stack");
    const mutate = async (operation, body = {}) => {
      await api(`/api/v1/knowledge/packs/${pack.id}/${operation}`, { method: "POST", body: JSON.stringify(body) });
      await load("packs");
    };
    add(controls,
      action("Check update", () => mutate("update-check")),
      action("Download", () => mutate("pull")),
      action("Rollback", () => mutate("rollback")),
      action("Refresh dry-run", () => mutate("refresh", { fetch: true, dry_run: true, revalidate: false })),
      action("Refresh", () => mutate("refresh", { fetch: true, dry_run: false, revalidate: false })),
      action("Refresh + revalidate", () => mutate("refresh", { fetch: true, dry_run: false, revalidate: true })),
    );
    (pack.installed_versions || []).forEach((release) => controls.appendChild(action(
      `Activate ${release.version}`,
      () => mutate("activate", { version: release.version }),
      pack.active?.version !== release.version,
    )));
    card.appendChild(controls);
  }
  if (pack.sources?.length) {
    card.appendChild(section("Authority sources"));
    pack.sources.forEach((source) => {
      const sourceNode = add(
        el("div", "source"),
        add(el("div", "record-head"), el("strong", "", source.id), status(source.health)),
        kv([
        ["Endpoint", safeLink(source.canonical_url)],
        ["Authority", source.authoritative_host],
        ["Type", source.source_type],
        ["Revision", source.revision || "—"],
        ["Last checked", fmt(source.last_checked)],
        ["Content hash", copyValue(source.content_sha256, true)],
        ]),
      );
      if (source.health === "unhealthy" && source.content_sha256) {
        sourceNode.appendChild(el("p", "compact muted", "Latest source check failed. Last-known-good revision and Evidence remain active."));
      }
      card.appendChild(sourceNode);
    });
  }
  return card;
}

function renderPacks(data) {
  const root = el("div");
  const order = { managed: 0, private: 1, builtin: 2 };
  const packs = [...data.packs].sort((left, right) => (order[left.category] - order[right.category]) || left.id.localeCompare(right.id));
  root.appendChild(searchable(packs, {
    placeholder: "Filter by Pack, owner, source, or version",
    statuses: ["managed", "private", "builtin"],
    statusValue: (pack) => pack.category,
    searchText: (pack) => [pack.id, pack.owner, pack.description, pack.category, pack.active?.version, ...(pack.sources || []).map((source) => `${source.id} ${source.revision}`)].join(" "),
    emptyText: "No Knowledge Packs match this filter.",
    render: packCard,
  }));
  content.replaceChildren(root);
}

function evidenceCard(item) {
  return add(
    el("article", "record"),
    add(el("div", "record-head"), el("h3", "", item.extracted_fact || "Source-derived evidence"), status(item.redacted ? "REDACTED" : "EVIDENCE")),
    kv([
      ["Evidence ID", copyValue(item.id)],
      ["Verification", item.verification_id ? textButton(item.verification_id, "verifications", item.verification_id) : "—"],
      ["Pack / source", `${item.pack_id || "—"} / ${item.source_id}`],
      ["Owner / type", `${item.owner} / ${item.source_type}`],
      ["Canonical source", safeLink(item.canonical_url)],
      ["Retrieved", fmt(item.retrieved_at)],
      ["Revision", item.document_revision || "—"],
      ["Content SHA-256", copyValue(item.content_sha256)],
    ]),
    ...(item.verification_links || []).map((link) => add(
      el("div", "source"),
      add(el("div", "record-head"), el("strong", "", link.statement), status(link.status)),
      kv([["Artifact", `${link.artifact_path}:${link.line}`], ["Verification", textButton(link.verification_id, "verifications", link.verification_id)], ["Recorded", fmt(link.recorded_at)]]),
    )),
  );
}

function renderEvidence(data) {
  const root = el("div");
  root.appendChild(section("Claim → Verification → Evidence → Source → Revision"));
  root.appendChild(remoteSearchable(data, {
    endpoint: "/api/v1/evidence",
    items: (payload) => payload.evidence,
    placeholder: "Filter by claim, Pack, source, revision, or Evidence ID",
    searchText: (item) => [item.id, item.pack_id, item.source_id, item.owner, item.document_revision, item.extracted_fact, ...(item.verification_links || []).map((link) => `${link.statement} ${link.status} ${link.artifact_path}`)].join(" "),
    emptyText: "No versioned Evidence records match this filter.",
    render: evidenceCard,
  }));
  content.replaceChildren(root);
}

function verificationCard(record) {
  const details = el("details", "detail");
  const statuses = (record.claims || []).map((claim) => claim.status);
  const dominant = statuses.includes("CONFLICT") ? "CONFLICT" : statuses.includes("UNVERIFIED") ? "UNVERIFIED" : statuses[0] || "UNKNOWN";
  add(details,
    add(el("summary"), add(el("div"), el("strong", "mono", record.id), el("div", "compact muted", (record.artifacts || []).join(", ") || "No artifact")), el("span", "muted", fmt(record.recorded_at)), status(dominant)),
    add(el("div", "detail-body"), kv([
      ["Verification ID", copyValue(record.id)],
      ["Report ID", copyValue(record.report_id)],
      ["Parent", record.parent_verification_id ? copyValue(record.parent_verification_id) : "Initial record"],
      ["Pack", record.pack_id || "—"],
      ["Scope", record.scope],
      ["Git", `${record.repository?.git_branch || "—"} · ${shortHash(record.repository?.git_commit)}`],
    ]), ...(record.claims || []).map((claim) => add(el("div", "source"), add(el("div", "record-head"), el("strong", "", claim.statement), status(claim.status)), kv([
      ["Claim ID", copyValue(claim.id)],
      ["Artifact", `${claim.artifact_path}:${claim.line}`],
      ["Confidence", claim.confidence],
      ["Reason", claim.reason],
      ["Evidence", claim.evidence?.length ? textButton(`${claim.evidence.length} linked record(s)`, "evidence", claim.evidence[0].evidence_id) : "—"],
    ])))),
  );
  return details;
}

function renderVerifications(data) {
  const root = remoteSearchable(data, {
    endpoint: "/api/v1/verifications",
    items: (payload) => payload.records,
    placeholder: "Filter by Verification ID, Pack, claim, artifact, or status",
    statuses: ["VERIFIED", "CONFLICT", "UNVERIFIED"],
    statusValue: (record) => (record.claims || []).some((claim) => claim.status === "CONFLICT") ? "CONFLICT" : (record.claims || []).some((claim) => claim.status === "UNVERIFIED") ? "UNVERIFIED" : "VERIFIED",
    searchText: (record) => [record.id, record.report_id, record.pack_id, ...(record.artifacts || []), ...(record.claims || []).map((claim) => `${claim.id} ${claim.statement} ${claim.status}`)].join(" "),
    emptyText: "No verification records match this filter.",
    render: verificationCard,
  });
  content.replaceChildren(root);
}

function renderProvenance(data) {
  const root = remoteSearchable(data, {
    endpoint: "/api/v1/provenance",
    items: (payload) => payload.records,
    placeholder: "Filter by Provenance ID, artifact, Pack, commit, or verification",
    searchText: (item) => [item.provenance_id, item.repository?.git_commit, item.repository?.git_branch, item.verification?.record_id, ...(item.knowledge_pack_ids || []), ...(item.evidence_ids || []), ...(item.artifacts || []).map((artifact) => artifact.path)].join(" "),
    emptyText: "No Engineering Provenance records match this filter.",
    render: (item) => provenanceCard(item),
  });
  root.classList.add("timeline");
  content.replaceChildren(root);
}

function renderAudit(data) {
  const root = remoteSearchable(data, {
    endpoint: "/api/v1/audit",
    items: (payload) => payload.events,
    placeholder: "Filter by event, Pack, source, artifact, or verification",
    searchText: (item) => [item.id, item.event, item.pack_id, item.source_id, item.artifact_path, item.verification_id, item.old_status, item.new_status].join(" "),
    emptyText: "No audit events match this filter.",
    render: auditCard,
  });
  root.classList.add("timeline");
  content.replaceChildren(root);
}

function renderGovernance(data) {
  const root = el("div");
  const policyBody = el("div");
  const select = el("select");
  select.setAttribute("aria-label", "Managed Pack update policy");
  ["manual", "auto-download", "auto-update"].forEach((value) => {
    const item = option(value, value);
    item.selected = data.update_policy === value;
    select.appendChild(item);
  });
  add(policyBody, el("p", "muted", "This local policy does not enable a background scheduler. Network access remains explicit."), add(
    el("div", "stack"), select,
    action("Save local policy", async () => {
      await api("/api/v1/governance/update-policy", { method: "POST", body: JSON.stringify({ policy: select.value }) });
      await load("governance");
    }, true),
  ));
  const publisherBody = el("div");
  if (!data.trusted_publishers.length) publisherBody.appendChild(empty("No managed remote publisher is configured."));
  data.trusted_publishers.forEach((item) => publisherBody.appendChild(add(el("article", "record"), el("h3", "", item.publisher), kv([
    ["Remote", item.remote_id], ["Base URL", safeLink(item.base_url)], ["Key ID", copyValue(item.key_id)],
  ]))));
  root.appendChild(add(el("div", "grid two-column"), panel("Managed Pack update policy", policyBody), panel("Trusted publishers", publisherBody)));
  root.appendChild(section("Project-local policy"));
  root.appendChild(action("Validate local Packs", async () => {
    const result = await api("/api/v1/knowledge/validate");
    return `Validated ${result.pack_count} Pack(s), including ${result.project_pack_count} project Pack(s).`;
  }, true));
  const privatePacks = data.packs.filter((pack) => pack.category === "private");
  if (!privatePacks.length) root.appendChild(empty("No project-local Packs are configured for this repository."));
  privatePacks.forEach((pack) => root.appendChild(packCard(pack, true)));
  content.replaceChildren(root);
}

const views = {
  overview: ["Overview", () => api("/api/v1/overview"), renderOverview],
  packs: ["Knowledge", () => api("/api/v1/knowledge/packs"), renderPacks],
  evidence: ["Evidence", () => api(`/api/v1/evidence?limit=${PAGE_SIZE}`), renderEvidence],
  verifications: ["Verification", () => api(`/api/v1/verifications?limit=${PAGE_SIZE}`), renderVerifications],
  provenance: ["Provenance", () => api(`/api/v1/provenance?limit=${PAGE_SIZE}`), renderProvenance],
  audit: ["Audit", () => api(`/api/v1/audit?limit=${PAGE_SIZE}`), renderAudit],
  governance: ["Governance", () => api("/api/v1/knowledge/packs"), renderGovernance],
};

async function load(view) {
  state.activeView = view;
  document.querySelectorAll(".nav-item").forEach((button) => {
    const active = button.dataset.view === view;
    button.classList.toggle("active", active);
    if (active) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  const [label, loader, render] = views[view];
  pageTitle.textContent = label;
  content.replaceChildren(el("div", "loading", "Loading customer-local state…"));
  try {
    render(await loader());
    showNotice("");
  } catch (error) {
    content.replaceChildren(empty("Customer-local state could not be rendered."));
    showNotice(error.message, true);
  }
}

document.querySelectorAll(".nav-item").forEach((button) => button.addEventListener("click", () => load(button.dataset.view)));
(async () => {
  try {
    state.csrf = (await api("/api/v1/session")).csrf_token;
    await load("overview");
  } catch (error) {
    showNotice(error.message, true);
  }
})();
