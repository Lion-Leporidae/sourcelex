// Sourcelex Explorer - 代码图谱浏览器
(function () {
  "use strict";

  // ===== State =====
  const state = {
    graphData: null,
    simulation: null,
    selectedNode: null,
    zoomBehavior: null,
    gElement: null,
  };

  const colorMap = { function: "#00d4ff", class: "#00ff88", method: "#a855f7" };
  const sizeMap = { function: 7, class: 11, method: 6 };

  // ===== Init =====
  document.addEventListener("DOMContentLoaded", function () {
    loadStats();
    loadFileTree();
    loadGraphData();
    initSearch();
    initResizers();
    initToolbar();

    document.getElementById("popup-close").addEventListener("click", closePopup);
  });

  // ===== Stats =====
  function loadStats() {
    fetch("/agent/stats").then(r => r.json()).then(data => {
      document.getElementById("stat-nodes").textContent = (data.node_count || 0) + " 实体";
      document.getElementById("stat-edges").textContent = (data.edge_count || 0) + " 调用";
    }).catch(() => {});
  }

  // ===== File Tree =====
  function loadFileTree() {
    const container = document.getElementById("file-tree");
    fetch("/api/v1/file/tree").then(r => r.json()).then(resp => {
      if (!resp.success) { container.innerHTML = '<div class="tree-loading">加载失败</div>'; return; }
      container.innerHTML = "";
      renderTreeNode(container, resp.data, 0);
    }).catch(() => { container.innerHTML = '<div class="tree-loading">加载失败</div>'; });
  }

  function renderTreeNode(parent, node, depth) {
    if (!node.children && !node.is_dir) {
      // File
      const item = document.createElement("div");
      item.className = "tree-item";
      item.innerHTML = '<span class="tree-indent" style="width:' + (depth * 16) + 'px"></span>' +
        '<span class="tree-icon file">📄</span>' +
        '<span class="tree-name">' + esc(node.name) + '</span>';
      item.addEventListener("click", function () {
        document.querySelectorAll(".tree-item.active").forEach(el => el.classList.remove("active"));
        item.classList.add("active");
        loadFileContent(node.path);
      });
      parent.appendChild(item);
      return;
    }

    // Directory
    const item = document.createElement("div");
    item.className = "tree-item";
    item.innerHTML = '<span class="tree-indent" style="width:' + (depth * 16) + 'px"></span>' +
      '<span class="tree-icon dir">▸</span>' +
      '<span class="tree-name">' + esc(node.name) + '</span>';

    const childContainer = document.createElement("div");
    childContainer.className = "tree-children";

    item.addEventListener("click", function () {
      const icon = item.querySelector(".tree-icon");
      const isOpen = childContainer.classList.toggle("open");
      icon.textContent = isOpen ? "▾" : "▸";
    });

    parent.appendChild(item);
    parent.appendChild(childContainer);

    if (node.children) {
      // Sort: dirs first, then files
      const sorted = node.children.slice().sort((a, b) => {
        if (a.is_dir && !b.is_dir) return -1;
        if (!a.is_dir && b.is_dir) return 1;
        return a.name.localeCompare(b.name);
      });
      sorted.forEach(child => renderTreeNode(childContainer, child, depth + 1));
    }
  }

  // ===== File Content Viewer =====
  function loadFileContent(filePath, startLine, endLine) {
    const viewer = document.getElementById("code-viewer");
    const title = document.getElementById("code-title");
    title.textContent = filePath;

    let url = "/api/v1/file/lines?path=" + encodeURIComponent(filePath);
    if (startLine) url += "&start=" + startLine;
    if (endLine) url += "&end=" + endLine;

    fetch(url).then(r => r.json()).then(resp => {
      if (!resp.success) { viewer.innerHTML = '<div class="code-placeholder"><p>' + esc(resp.error) + '</p></div>'; return; }
      const data = resp.data;
      const ext = filePath.split(".").pop();
      const langMap = { go: "go", py: "python", js: "javascript", ts: "typescript", java: "java", c: "c", cpp: "cpp", h: "c", hpp: "cpp" };
      const lang = langMap[ext] || "";

      let html = '<div class="code-file-header"><span class="code-file-path">' + esc(filePath) + '</span>';
      html += '<span>(' + data.start_line + '-' + data.end_line + ' / ' + data.total_lines + ' 行)</span></div>';
      html += '<div class="code-lines">';

      const lines = data.lines || [];
      const hlStart = startLine || 0;
      const hlEnd = endLine || 0;

      for (let i = 0; i < lines.length; i++) {
        const lineNum = (data.start_line || 1) + i;
        const isHL = hlStart && hlEnd && lineNum >= hlStart && lineNum <= hlEnd;
        let content = esc(lines[i]);
        if (lang) {
          try { content = hljs.highlight(lines[i], { language: lang }).value; } catch (_) {}
        }
        html += '<div class="code-line' + (isHL ? ' highlighted' : '') + '">' +
          '<span class="line-number">' + lineNum + '</span>' +
          '<span class="line-content">' + content + '</span></div>';
      }
      html += '</div>';
      viewer.innerHTML = html;
    }).catch(() => {
      viewer.innerHTML = '<div class="code-placeholder"><p>加载文件失败</p></div>';
    });
  }

  // ===== Search =====
  function initSearch() {
    const input = document.getElementById("search-input");
    const results = document.getElementById("search-results");
    let timer = null;

    input.addEventListener("input", function () {
      clearTimeout(timer);
      const q = input.value.trim();
      if (q.length < 2) { results.classList.add("hidden"); return; }
      timer = setTimeout(() => doSearch(q), 300);
    });

    input.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { results.classList.add("hidden"); }
    });

    document.addEventListener("click", function (e) {
      if (!results.contains(e.target) && e.target !== input) results.classList.add("hidden");
    });
  }

  function doSearch(query) {
    const results = document.getElementById("search-results");
    fetch("/api/v1/search/semantic", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: query, top_k: 8 }),
    }).then(r => r.json()).then(resp => {
      if (!resp.success || !resp.data || resp.data.length === 0) {
        results.innerHTML = '<div class="search-result-item"><span class="search-result-info">无结果</span></div>';
        results.classList.remove("hidden");
        return;
      }
      results.innerHTML = resp.data.map(r =>
        '<div class="search-result-item" data-id="' + esc(r.entity_id) + '">' +
        '<div class="search-result-name">' + esc(r.entity_id) + '</div>' +
        '<div class="search-result-info">' + esc(r.type || "") + ' · ' + esc(r.file_path || "") +
        ' · 相似度 ' + (r.score * 100).toFixed(0) + '%</div></div>'
      ).join("");
      results.classList.remove("hidden");

      results.querySelectorAll(".search-result-item").forEach(item => {
        item.addEventListener("click", function () {
          const id = item.dataset.id;
          results.classList.add("hidden");
          focusNode(id);
        });
      });
    }).catch(() => {});
  }

  // ===== Graph =====
  function loadGraphData() {
    fetch("/agent/graph/data").then(r => r.json()).then(data => {
      state.graphData = data;
      renderGraph(data);
    }).catch(err => console.error("Graph load failed:", err));
  }

  function renderGraph(data) {
    const viewport = document.getElementById("graph-viewport");
    const svg = d3.select("#graph-svg");
    svg.selectAll("*").remove();

    if (!data.nodes || data.nodes.length === 0) {
      svg.append("text").attr("x", "50%").attr("y", "50%").attr("text-anchor", "middle")
        .attr("fill", "#6e7681").attr("font-size", "14px")
        .text("暂无数据，请先索引代码库");
      return;
    }

    const width = viewport.clientWidth;
    const height = viewport.clientHeight;

    // Tooltip
    let tooltip = d3.select(".graph-tooltip");
    if (tooltip.empty()) {
      tooltip = d3.select("body").append("div").attr("class", "graph-tooltip").style("display", "none");
    }

    const g = svg.append("g");
    state.gElement = g;

    // Zoom
    state.zoomBehavior = d3.zoom().scaleExtent([0.05, 5])
      .on("zoom", event => g.attr("transform", event.transform));
    svg.call(state.zoomBehavior);

    // ===== Group nodes by file =====
    const fileGroups = {};
    data.nodes.forEach(n => {
      const f = n.file_path || "(unknown)";
      if (!fileGroups[f]) fileGroups[f] = [];
      fileGroups[f].push(n);
    });

    const fileNames = Object.keys(fileGroups).sort();
    const nodeIdToFile = {};
    data.nodes.forEach(n => { nodeIdToFile[n.id] = n.file_path || "(unknown)"; });

    // Layout: arrange file boxes in a grid
    const boxPadX = 16, boxPadY = 30, nodeH = 22, nodeW = 180;
    const gapX = 60, gapY = 40;
    const cols = Math.max(1, Math.ceil(Math.sqrt(fileNames.length)));

    const fileBoxes = {}; // fileName -> { x, y, w, h, nodes: [{x,y,node}] }
    let col = 0, row = 0, maxRowH = 0;
    let cx = 30, cy = 30;

    fileNames.forEach(fn => {
      const nodes = fileGroups[fn];
      const boxW = nodeW + boxPadX * 2;
      const boxH = boxPadY + nodes.length * nodeH + 10;

      if (col >= cols) { col = 0; row++; cx = 30; cy += maxRowH + gapY; maxRowH = 0; }

      const box = { x: cx, y: cy, w: boxW, h: boxH, nodes: [] };
      nodes.forEach((n, i) => {
        const nx = cx + boxPadX + nodeW / 2;
        const ny = cy + boxPadY + i * nodeH + nodeH / 2;
        n.fx = nx; n.fy = ny; n.bx = cx; n.by = cy;
        box.nodes.push({ x: nx, y: ny, node: n });
      });
      fileBoxes[fn] = box;
      cx += boxW + gapX;
      maxRowH = Math.max(maxRowH, boxH);
      col++;
    });

    // Arrow defs
    const defs = svg.append("defs");
    defs.append("marker").attr("id", "arrow")
      .attr("viewBox", "0 -5 10 10").attr("refX", 8).attr("refY", 0)
      .attr("markerWidth", 6).attr("markerHeight", 6).attr("orient", "auto")
      .append("path").attr("d", "M0,-3L8,0L0,3").attr("fill", "#484f58");
    defs.append("marker").attr("id", "arrow-cross")
      .attr("viewBox", "0 -5 10 10").attr("refX", 8).attr("refY", 0)
      .attr("markerWidth", 6).attr("markerHeight", 6).attr("orient", "auto")
      .append("path").attr("d", "M0,-3L8,0L0,3").attr("fill", "#f0883e");

    // Draw file boxes
    const fileBoxGroup = g.append("g").attr("class", "file-boxes");
    fileNames.forEach(fn => {
      const box = fileBoxes[fn];
      const shortName = fn.split("/").pop();

      fileBoxGroup.append("rect")
        .attr("x", box.x).attr("y", box.y)
        .attr("width", box.w).attr("height", box.h)
        .attr("rx", 6).attr("ry", 6)
        .attr("fill", "#161b22").attr("stroke", "#30363d").attr("stroke-width", 1);

      fileBoxGroup.append("text")
        .attr("x", box.x + 8).attr("y", box.y + 16)
        .attr("fill", "#58a6ff").attr("font-size", "11px").attr("font-weight", "600")
        .text(shortName)
        .attr("cursor", "pointer")
        .on("click", () => loadFileContent(fn));
    });

    // Filter valid edges + classify
    const nodeIds = new Set(data.nodes.map(n => n.id));
    const edges = data.edges.filter(e => nodeIds.has(e.source) && nodeIds.has(e.target));

    // Node id → position
    const nodePos = {};
    data.nodes.forEach(n => { nodePos[n.id] = { x: n.fx, y: n.fy }; });

    // Draw edges — curve for cross-file, straight for same-file
    const linkGroup = g.append("g").attr("class", "links");
    edges.forEach(e => {
      const srcId = typeof e.source === "object" ? e.source.id : e.source;
      const tgtId = typeof e.target === "object" ? e.target.id : e.target;
      const src = nodePos[srcId], tgt = nodePos[tgtId];
      if (!src || !tgt) return;

      const srcFile = nodeIdToFile[srcId];
      const tgtFile = nodeIdToFile[tgtId];
      const isCross = srcFile !== tgtFile;

      if (isCross) {
        // Curved path for cross-file
        const mx = (src.x + tgt.x) / 2;
        const my = (src.y + tgt.y) / 2;
        const dx = tgt.x - src.x, dy = tgt.y - src.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        const offset = Math.min(dist * 0.3, 80);
        const nx = -dy / dist * offset, ny = dx / dist * offset;

        linkGroup.append("path")
          .attr("d", "M" + src.x + "," + src.y + " Q" + (mx + nx) + "," + (my + ny) + " " + tgt.x + "," + tgt.y)
          .attr("fill", "none")
          .attr("stroke", "#f0883e").attr("stroke-width", 1.5)
          .attr("stroke-dasharray", "6,3")
          .attr("marker-end", "url(#arrow-cross)")
          .attr("opacity", 0.8)
          .datum({ srcId, tgtId, isCross: true });
      } else {
        // Straight line for same-file
        linkGroup.append("line")
          .attr("x1", src.x).attr("y1", src.y)
          .attr("x2", tgt.x).attr("y2", tgt.y)
          .attr("stroke", "#484f58").attr("stroke-width", 0.8)
          .attr("marker-end", "url(#arrow)")
          .attr("opacity", 0.5)
          .datum({ srcId, tgtId, isCross: false });
      }
    });

    // Draw nodes (circles + labels)
    const nodeGroup = g.append("g").attr("class", "nodes");
    data.nodes.forEach(n => {
      nodeGroup.append("circle")
        .attr("cx", n.fx).attr("cy", n.fy)
        .attr("r", sizeMap[n.type] || 6)
        .attr("fill", colorMap[n.type] || "#58a6ff")
        .attr("stroke", "#0d1117").attr("stroke-width", 1.5)
        .attr("cursor", "pointer")
        .datum(n)
        .on("mouseover", function (event, d) {
          tooltip.style("display", "block").html(
            '<div class="tooltip-name">' + esc(d.name) + '</div>' +
            '<div class="tooltip-type">' + esc(d.type) + '</div>' +
            '<div class="tooltip-file">' + esc(d.file_path || "") + ':' + (d.start_line || "") + '</div>' +
            (d.signature ? '<div class="tooltip-sig">' + esc(d.signature) + '</div>' : "")
          );
        })
        .on("mousemove", function (event) {
          tooltip.style("left", (event.pageX + 14) + "px").style("top", (event.pageY - 8) + "px");
        })
        .on("mouseout", function () { tooltip.style("display", "none"); })
        .on("click", function (event, d) {
          event.stopPropagation();
          showNodePopup(d);
        });

      nodeGroup.append("text")
        .attr("x", n.fx + (sizeMap[n.type] || 6) + 4).attr("y", n.fy + 4)
        .attr("fill", "#8b949e").attr("font-size", "10px")
        .attr("pointer-events", "none")
        .text(n.name);
    });

    svg.on("click", () => closePopup());

    // Center view on content
    const totalW = cx + 200, totalH = cy + maxRowH + 100;
    const scaleX = width / totalW, scaleY = height / totalH;
    const initScale = Math.min(scaleX, scaleY, 1) * 0.9;
    svg.call(state.zoomBehavior.transform,
      d3.zoomIdentity.translate(20, 20).scale(initScale));

    // No simulation needed — static layout
    state.simulation = null;
  }

  // ===== Node Popup =====
  function showNodePopup(d) {
    state.selectedNode = d;
    const popup = document.getElementById("node-popup");
    document.getElementById("popup-title").textContent = d.name;
    document.getElementById("popup-body").innerHTML =
      '<div class="detail-row"><span class="detail-label">类型</span><span class="detail-value">' + esc(d.type) + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">ID</span><span class="detail-value">' + esc(d.id) + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">文件</span><span class="detail-value">' + esc(d.file_path || "") + '</span></div>' +
      '<div class="detail-row"><span class="detail-label">行号</span><span class="detail-value">' + (d.start_line || "") + ' - ' + (d.end_line || "") + '</span></div>' +
      (d.signature ? '<div class="detail-row"><span class="detail-label">签名</span><span class="detail-value">' + esc(d.signature) + '</span></div>' : "");

    popup.classList.remove("hidden");

    // Bind actions
    document.getElementById("popup-view-code").onclick = function () {
      closePopup();
      if (d.file_path) loadFileContent(d.file_path, d.start_line, d.end_line);
    };
    document.getElementById("popup-view-callers").onclick = function () {
      closePopup();
      highlightRelated(d.id, "callers");
    };
    document.getElementById("popup-view-callees").onclick = function () {
      closePopup();
      highlightRelated(d.id, "callees");
    };

    // Cross-file call chain button
    const chainBtn = document.getElementById("popup-view-chain");
    if (chainBtn) {
      chainBtn.onclick = function () {
        closePopup();
        showCrossFileChain(d.id, d.name);
      };
    }
  }

  function closePopup() {
    document.getElementById("node-popup").classList.add("hidden");
  }

  // ===== Highlight Related Nodes =====
  function highlightRelated(entityId, direction) {
    const url = direction === "callers"
      ? "/api/v1/callers/" + encodeURIComponent(entityId) + "?depth=2"
      : "/api/v1/callees/" + encodeURIComponent(entityId) + "?depth=2";

    fetch(url).then(r => r.json()).then(resp => {
      if (!resp.success || !resp.data) return;
      const relatedIds = new Set(resp.data.map(n => n.id));
      relatedIds.add(entityId);

      d3.selectAll(".nodes circle")
        .attr("opacity", d => relatedIds.has(d.id) ? 1 : 0.12)
        .attr("r", d => relatedIds.has(d.id) ? (sizeMap[d.type] || 6) + 3 : sizeMap[d.type] || 6)
        .attr("stroke", d => d.id === entityId ? "#f0883e" : "#0d1117")
        .attr("stroke-width", d => d.id === entityId ? 3 : 1.5);

      d3.selectAll(".nodes text")
        .attr("opacity", d => relatedIds.has(d.id) ? 1 : 0.08);

      d3.selectAll(".links path, .links line")
        .attr("opacity", function () {
          const dd = d3.select(this).datum();
          if (!dd) return 0.05;
          return relatedIds.has(dd.srcId) && relatedIds.has(dd.tgtId) ? 1 : 0.05;
        });

      d3.selectAll(".file-boxes rect").attr("opacity", 0.4);
      d3.selectAll(".file-boxes text").attr("opacity", 0.4);

      document.getElementById("graph-title").textContent =
        direction === "callers" ? entityId + " 的调用者" : entityId + " 的被调用者";

      d3.select("#graph-svg").on("click.reset", function () {
        resetHighlight();
        d3.select("#graph-svg").on("click.reset", null);
      });
    });
  }

  function resetHighlight() {
    d3.selectAll(".nodes circle")
      .attr("opacity", 1)
      .attr("r", d => sizeMap[d ? d.type : "function"] || 6)
      .attr("stroke", "#0d1117").attr("stroke-width", 1.5);
    d3.selectAll(".nodes text").attr("opacity", 1);
    d3.selectAll(".links path, .links line").attr("opacity", function () {
      const dd = d3.select(this).datum();
      return dd && dd.isCross ? 0.8 : 0.5;
    });
    d3.selectAll(".file-boxes rect").attr("opacity", 1);
    d3.selectAll(".file-boxes text").attr("opacity", 1);
    document.getElementById("graph-title").textContent = "调用图谱";
  }

  // ===== Cross-File Call Chain =====
  function showCrossFileChain(entityId, entityName) {
    // Fetch callers (depth 3) and callees (depth 3) to build a chain
    Promise.all([
      fetch("/api/v1/callers/" + encodeURIComponent(entityId) + "?depth=3").then(r => r.json()),
      fetch("/api/v1/callees/" + encodeURIComponent(entityId) + "?depth=3").then(r => r.json()),
    ]).then(([callersResp, calleesResp]) => {
      const callers = (callersResp.success && callersResp.data) ? callersResp.data : [];
      const callees = (calleesResp.success && calleesResp.data) ? calleesResp.data : [];

      // Collect all related IDs
      const relatedIds = new Set([entityId]);
      callers.forEach(n => relatedIds.add(n.id));
      callees.forEach(n => relatedIds.add(n.id));

      // Find cross-file nodes only
      const centerNode = state.graphData ? state.graphData.nodes.find(n => n.id === entityId) : null;
      const centerFile = centerNode ? centerNode.file_path : "";
      const crossFileCallers = callers.filter(n => n.file_path !== centerFile);
      const crossFileCallees = callees.filter(n => n.file_path !== centerFile);

      // Highlight cross-file chain
      const highlightIds = new Set([entityId]);
      crossFileCallers.forEach(n => highlightIds.add(n.id));
      crossFileCallees.forEach(n => highlightIds.add(n.id));

      d3.selectAll(".nodes circle")
        .attr("opacity", d => highlightIds.has(d.id) ? 1 : 0.1)
        .attr("r", d => highlightIds.has(d.id) ? (sizeMap[d.type] || 6) + 4 : sizeMap[d.type] || 6)
        .attr("stroke", d => d.id === entityId ? "#f0883e" : "#0d1117")
        .attr("stroke-width", d => d.id === entityId ? 3 : 1.5);

      d3.selectAll(".links path, .links line")
        .attr("opacity", function () {
          const dd = d3.select(this).datum();
          if (!dd) return 0.03;
          return highlightIds.has(dd.srcId) && highlightIds.has(dd.tgtId) ? 1 : 0.03;
        })
        .attr("stroke-width", function () {
          const dd = d3.select(this).datum();
          if (!dd) return 1;
          return highlightIds.has(dd.srcId) && highlightIds.has(dd.tgtId) ? 2.5 : 1;
        });

      d3.selectAll(".nodes text")
        .attr("opacity", d => highlightIds.has(d.id) ? 1 : 0.05);

      d3.selectAll(".file-boxes rect").attr("opacity", 0.3);
      d3.selectAll(".file-boxes text").attr("opacity", 0.3);

      document.getElementById("graph-title").textContent =
        entityName + " 的跨文件调用链 (" + crossFileCallers.length + " 调用者, " + crossFileCallees.length + " 被调用)";

      // Show chain details in code viewer
      let chainHtml = '<div class="code-file-header"><span class="code-file-path">跨文件调用链: ' + esc(entityName) + '</span></div>';
      chainHtml += '<div class="code-lines">';

      if (crossFileCallers.length > 0) {
        chainHtml += '<div class="code-line highlighted"><span class="line-content" style="color:#f0883e;font-weight:bold">── 调用者（其他文件 → 本函数）──</span></div>';
        crossFileCallers.forEach(n => {
          chainHtml += '<div class="code-line" style="cursor:pointer" onclick="window.__loadFileContent(\'' +
            esc(n.file_path) + '\',' + (n.start_line || 1) + ',' + (n.end_line || 0) + ')">' +
            '<span class="line-number" style="color:#3fb950">' + esc(n.type) + '</span>' +
            '<span class="line-content">' + esc(n.id) + ' <span style="color:#6e7681">(' + esc(n.file_path) + ':' + (n.start_line || '') + ')</span></span></div>';
        });
      }

      chainHtml += '<div class="code-line" style="background:#161b22"><span class="line-content" style="color:#58a6ff;font-weight:bold">● ' + esc(entityId) + ' (' + esc(centerFile) + ')</span></div>';

      if (crossFileCallees.length > 0) {
        chainHtml += '<div class="code-line highlighted"><span class="line-content" style="color:#f0883e;font-weight:bold">── 被调用（本函数 → 其他文件）──</span></div>';
        crossFileCallees.forEach(n => {
          chainHtml += '<div class="code-line" style="cursor:pointer" onclick="window.__loadFileContent(\'' +
            esc(n.file_path) + '\',' + (n.start_line || 1) + ',' + (n.end_line || 0) + ')">' +
            '<span class="line-number" style="color:#bc8cff">' + esc(n.type) + '</span>' +
            '<span class="line-content">' + esc(n.id) + ' <span style="color:#6e7681">(' + esc(n.file_path) + ':' + (n.start_line || '') + ')</span></span></div>';
        });
      }

      if (crossFileCallers.length === 0 && crossFileCallees.length === 0) {
        chainHtml += '<div class="code-line"><span class="line-content" style="color:#6e7681">该函数没有跨文件调用关系</span></div>';
      }

      chainHtml += '</div>';
      document.getElementById("code-viewer").innerHTML = chainHtml;
      document.getElementById("code-title").textContent = "跨文件调用链";

      d3.select("#graph-svg").on("click.reset", function () {
        resetHighlight();
        d3.select("#graph-svg").on("click.reset", null);
      });
    });
  }

  // Expose loadFileContent for inline onclick handlers
  window.__loadFileContent = loadFileContent;

  function focusNode(entityId) {
    if (!state.graphData) return;
    const node = state.graphData.nodes.find(n => n.id === entityId);
    if (node && node.fx != null) {
      const svg = d3.select("#graph-svg");
      const viewport = document.getElementById("graph-viewport");
      const w = viewport.clientWidth;
      const h = viewport.clientHeight;

      svg.transition().duration(500).call(
        state.zoomBehavior.transform,
        d3.zoomIdentity.translate(w / 2 - node.fx * 1.5, h / 2 - node.fy * 1.5).scale(1.5)
      );

      // Flash effect
      d3.selectAll(".nodes circle")
        .filter(d => d.id === entityId)
        .transition().duration(200).attr("r", 14)
        .transition().duration(400).attr("r", sizeMap[node.type] || 6);

      showNodePopup(node);
    }
  }

  // ===== Toolbar =====
  function initToolbar() {
    document.getElementById("btn-reset").addEventListener("click", function () {
      resetHighlight();
      if (state.graphData) renderGraph(state.graphData);
    });

    document.getElementById("btn-zoom-fit").addEventListener("click", function () {
      const svg = d3.select("#graph-svg");
      svg.transition().duration(300).call(state.zoomBehavior.transform, d3.zoomIdentity);
    });

    document.getElementById("btn-zoom-in").addEventListener("click", function () {
      d3.select("#graph-svg").transition().duration(200).call(state.zoomBehavior.scaleBy, 1.3);
    });

    document.getElementById("btn-zoom-out").addEventListener("click", function () {
      d3.select("#graph-svg").transition().duration(200).call(state.zoomBehavior.scaleBy, 0.7);
    });

    document.getElementById("graph-type-filter").addEventListener("change", function () {
      if (!state.graphData) return;
      const type = this.value;
      if (type === "all") { renderGraph(state.graphData); return; }

      const filtered = state.graphData.nodes.filter(n => n.type === type);
      const ids = new Set(filtered.map(n => n.id));
      const edges = state.graphData.edges.filter(e => ids.has(e.source) || ids.has(e.target) || ids.has(e.source?.id) || ids.has(e.target?.id));
      renderGraph({ nodes: filtered, edges: edges });
    });

    // Panel toggles
    document.getElementById("toggle-left").addEventListener("click", function () {
      const panel = document.getElementById("panel-left");
      const resizer = document.getElementById("resizer-left");
      const isHidden = panel.style.display === "none";
      panel.style.display = isHidden ? "" : "none";
      resizer.style.display = isHidden ? "" : "none";
      this.textContent = isHidden ? "◀" : "▶";
    });

    document.getElementById("toggle-right").addEventListener("click", function () {
      const panel = document.getElementById("panel-right");
      const resizer = document.getElementById("resizer-right");
      const isHidden = panel.style.display === "none";
      panel.style.display = isHidden ? "" : "none";
      resizer.style.display = isHidden ? "" : "none";
      this.textContent = isHidden ? "▶" : "◀";
    });
  }

  // ===== Resizers =====
  function initResizers() {
    makeResizable("resizer-left", "panel-left", "left");
    makeResizable("resizer-right", "panel-right", "right");
  }

  function makeResizable(resizerId, panelId, side) {
    const resizer = document.getElementById(resizerId);
    const panel = document.getElementById(panelId);
    let startX, startWidth;

    resizer.addEventListener("mousedown", function (e) {
      startX = e.clientX;
      startWidth = panel.getBoundingClientRect().width;
      resizer.classList.add("active");
      document.addEventListener("mousemove", onMouseMove);
      document.addEventListener("mouseup", onMouseUp);
      e.preventDefault();
    });

    function onMouseMove(e) {
      const diff = e.clientX - startX;
      const newWidth = side === "left" ? startWidth + diff : startWidth - diff;
      if (newWidth >= 150 && newWidth <= 600) {
        panel.style.width = newWidth + "px";
      }
    }

    function onMouseUp() {
      resizer.classList.remove("active");
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    }
  }

  // ===== Utils =====
  function esc(text) {
    const div = document.createElement("div");
    div.textContent = text || "";
    return div.innerHTML;
  }
})();
